package server_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/image/v2/images"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	singularImageSchemaPath   = "/image/v2/schemas/image"
	collectionImageSchemaPath = "/image/v2/schemas/images"
)

func TestGophercloudFollowsGlanceSchemaLinks(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	client, err := openstack.NewImageV2(provider, gophercloud.EndpointOpts{
		Region: "RegionOne", Availability: gophercloud.AvailabilityPublic,
	})
	require.NoError(t, err)
	ctx := t.Context()
	detail, err := images.Get(ctx, client, fixtureImageID).Extract()
	require.NoError(t, err)
	require.Equal(t, singularImageSchemaPath, detail.Schema)
	var page struct {
		Images []map[string]json.RawMessage `json:"images"`
		Schema string                       `json:"schema"`
	}
	_, err = client.Get(ctx, client.ServiceURL("images"), &page, nil)
	require.NoError(t, err)
	require.Equal(t, collectionImageSchemaPath, page.Schema)
	require.Len(t, page.Images, 1)

	follow := func(path string) json.RawMessage {
		t.Helper()
		var doc json.RawMessage
		response, err := client.Get(ctx, instance.URL+path, &doc, nil)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, response.StatusCode)
		requireJSON(t, response.Header)
		return doc
	}
	singular := follow(detail.Schema)
	collection := follow(page.Schema)
	checkImageSchemas(t, singular, collection, page.Images[0])
}

func TestGlanceImageSchemasWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	auth := map[string]string{"X-Auth-Token": provider.Token()}

	// https://docs.openstack.org/api-ref/image/v2/#schemas
	for _, schema := range []struct {
		path string
		name string
	}{
		{singularImageSchemaPath, "image"},
		{collectionImageSchemaPath, "images"},
	} {
		t.Run(schema.name, func(t *testing.T) {
			status, headers, body := request(t, instance, http.MethodGet, schema.path, "", nil)
			require.Equal(t, http.StatusUnauthorized, status, "%s", body)
			assert.Equal(t, "Keystone", headers.Get("WWW-Authenticate"))
			status, headers, body = request(t, instance, http.MethodGet, schema.path, "", auth)
			require.Equal(t, http.StatusOK, status, "%s", body)
			requireJSON(t, headers)
			var doc map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(body, &doc))
			assert.JSONEq(t, `"`+schema.name+`"`, string(doc["name"]))
			assert.NotContains(t, doc, "type")
			assert.NotContains(t, doc, "$schema")
		})
	}
}

func checkImageSchemas(
	t *testing.T, singular, collection json.RawMessage, image map[string]json.RawMessage,
) {
	t.Helper()
	type link struct {
		Rel  string `json:"rel"`
		Href string `json:"href"`
	}
	type property struct {
		Type  json.RawMessage `json:"type"`
		Items json.RawMessage `json:"items"`
	}
	var imageSchema, imagesSchema struct {
		Name                 string              `json:"name"`
		Properties           map[string]property `json:"properties"`
		Links                []link              `json:"links"`
		AdditionalProperties json.RawMessage     `json:"additionalProperties"`
	}
	require.NoError(t, json.Unmarshal(singular, &imageSchema))
	require.NoError(t, json.Unmarshal(collection, &imagesSchema))
	assert.Equal(t, "image", imageSchema.Name)
	assert.Equal(t, "images", imagesSchema.Name)
	assert.JSONEq(t, `{"type":"string"}`, string(imageSchema.AdditionalProperties))

	// https://github.com/openstack/glance/blob/master/glance/api/v2/images.py#L1962-L1967
	assert.ElementsMatch(t, []link{
		{Rel: "self", Href: "{self}"},
		{Rel: "enclosure", Href: "{file}"},
		{Rel: "describedby", Href: "{schema}"},
	}, imageSchema.Links)
	// https://github.com/openstack/glance/blob/master/glance/schema.py
	assert.ElementsMatch(t, []link{
		{Rel: "first", Href: "{first}"},
		{Rel: "next", Href: "{next}"},
		{Rel: "describedby", Href: "{schema}"},
	}, imagesSchema.Links)
	require.Len(t, imagesSchema.Properties, 4)
	for _, field := range []string{"first", "next", "schema"} {
		assert.JSONEq(t, `"string"`, string(imagesSchema.Properties[field].Type), "%s", field)
	}
	assert.JSONEq(t, `"array"`, string(imagesSchema.Properties["images"].Type))
	assert.JSONEq(t, string(singular), string(imagesSchema.Properties["images"].Items))

	require.Len(t, imageSchema.Properties, len(image))
	for field, rawValue := range image {
		require.Contains(t, imageSchema.Properties, field)
		declared := imageSchema.Properties[field]
		var accepted []string
		var single string
		if err := json.Unmarshal(declared.Type, &single); err == nil {
			accepted = []string{single}
		} else {
			require.NoError(t, json.Unmarshal(declared.Type, &accepted), "%s", field)
		}
		decoder := json.NewDecoder(bytes.NewReader(rawValue))
		decoder.UseNumber()
		var value any
		require.NoError(t, decoder.Decode(&value), "%s", field)
		switch value := value.(type) {
		case string:
			assert.Contains(t, accepted, "string", "%s", field)
		case bool:
			assert.Contains(t, accepted, "boolean", "%s", field)
		case json.Number:
			_, err := value.Int64()
			require.NoError(t, err, "%s", field)
			assert.Contains(t, accepted, "integer", "%s", field)
		case []any:
			assert.Contains(t, accepted, "array", "%s", field)
			assert.JSONEq(t, `{"type":"string","maxLength":255}`, string(declared.Items), "%s", field)
		case nil:
			assert.Contains(t, accepted, "null", "%s", field)
		default:
			t.Errorf("unexpected image field type for %s: %T", field, value)
		}
	}
}
