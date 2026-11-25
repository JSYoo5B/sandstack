package server_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/image/v2/images"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGophercloudFollowsGlanceImageListNextPage(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	client, err := openstack.NewImageV2(provider, gophercloud.EndpointOpts{
		Region:       "RegionOne",
		Availability: gophercloud.AvailabilityPublic,
	})
	require.NoError(t, err)
	ctx := t.Context()
	tags := []string{"page + one", "owner:test"}
	tagged, err := images.Update(ctx, client, fixtureImageID, images.UpdateOpts{
		images.ReplaceImageTags{NewTags: tags},
	}).Extract()
	require.NoError(t, err)

	pages, err := images.List(client, images.ListOpts{
		Limit: 1, Tags: tags, Name: tagged.Name, Status: tagged.Status,
	}).AllPages(ctx)
	require.NoError(t, err)
	rows, err := images.ExtractImages(pages)
	require.NoError(t, err)
	assert.Equal(t, []images.Image{*tagged}, rows)

	status, _, body := request(t, instance, http.MethodGet, "/_sandstack/requests", "", map[string]string{
		"X-SandStack-Admin-Token": adminToken,
	})
	require.Equal(t, http.StatusOK, status, "%s", body)
	type listRequest struct {
		Method string `json:"method"`
		Route  string `json:"route"`
		Status int    `json:"status"`
	}
	var journal struct {
		Requests []listRequest `json:"requests"`
	}
	require.NoError(t, json.Unmarshal(body, &journal))
	completed := listRequest{Method: http.MethodGet, Route: imagesPath, Status: http.StatusOK}
	require.GreaterOrEqual(t, len(journal.Requests), 2)
	assert.Equal(t, []listRequest{completed, completed}, journal.Requests[len(journal.Requests)-2:])
}

func TestGlanceImageListPaginationWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	const patchType = "application/openstack-images-v2.1-json-patch"
	patch := `[{"op":"replace","path":"/tags","value":["page + one","owner:test"]}]`
	status, _, body := request(t, instance, http.MethodPatch, imagesPath+"/"+fixtureImageID, patch, map[string]string{
		"X-Auth-Token": provider.Token(), "Content-Type": patchType,
	})
	require.Equal(t, http.StatusOK, status, "%s", body)
	detail := string(body)
	query := url.Values{
		"limit":  {"1"},
		"name":   {"eq:fixture-image"},
		"status": {"active"},
		"tag":    {"page + one", "owner:test"},
	}

	// https://docs.openstack.org/api-ref/image/v2/#list-images
	readPage := func(path string) map[string]json.RawMessage {
		t.Helper()
		status, headers, body := request(t, instance, http.MethodGet, path, "", auth)
		require.Equal(t, http.StatusOK, status, "%s", body)
		requireJSON(t, headers)
		var fields map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(body, &fields))
		assert.JSONEq(t, `"/image/v2/schemas/images"`, string(fields["schema"]))
		return fields
	}
	link := func(raw json.RawMessage, path string, want url.Values) {
		t.Helper()
		var href string
		require.NoError(t, json.Unmarshal(raw, &href))
		parsed, err := url.Parse(href)
		require.NoError(t, err)
		assert.Empty(t, parsed.Host)
		assert.Empty(t, parsed.Scheme)
		assert.Equal(t, path, parsed.Path)
		assert.Equal(t, want, parsed.Query())
	}

	first := readPage(imagesPath + "?" + query.Encode())
	assert.Len(t, first, 4)
	assert.JSONEq(t, "["+detail+"]", string(first["images"]))
	link(first["first"], imagesPath, query)
	withMarker := query.Encode() + "&marker=" + fixtureImageID
	nextQuery, err := url.ParseQuery(withMarker)
	require.NoError(t, err)
	link(first["next"], "/v2/images", nextQuery)

	terminal := readPage(imagesPath + "?" + withMarker)
	assert.Len(t, terminal, 3)
	assert.NotContains(t, terminal, "next")
	assert.JSONEq(t, "[]", string(terminal["images"]))
	link(terminal["first"], imagesPath, query)

	mismatched := readPage(imagesPath + "?limit=1&name=no-match&marker=" + fixtureImageID)
	assert.JSONEq(t, "[]", string(mismatched["images"]))
	assert.NotContains(t, mismatched, "next")

	zero := readPage(imagesPath + "?limit=0")
	assert.JSONEq(t, "[]", string(zero["images"]))
	assert.NotContains(t, zero, "next")
	link(zero["first"], imagesPath, url.Values{"limit": {"0"}})

	for _, rejected := range []string{
		"limit=-1", "limit=wrong", "limit=1&limit=1", "limit=",
		"marker=", "marker=00000000-0000-0000-0000-000000000000",
		"marker=" + fixtureImageID + "&marker=" + fixtureImageID,
	} {
		t.Run(rejected, func(t *testing.T) {
			status, _, body := request(t, instance, http.MethodGet, imagesPath+"?"+rejected, "", auth)
			assert.Equal(t, http.StatusBadRequest, status, "%s", body)
		})
	}
}
