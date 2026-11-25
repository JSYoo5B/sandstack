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

func TestGophercloudListsPrivateImageAcrossStatusChanges(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	client, err := openstack.NewImageV2(provider, gophercloud.EndpointOpts{
		Region: "RegionOne", Availability: gophercloud.AvailabilityPublic,
	})
	require.NoError(t, err)
	ctx := t.Context()
	get := func() *images.Image {
		t.Helper()
		result, err := images.Get(ctx, client, fixtureImageID).Extract()
		require.NoError(t, err)
		return result
	}
	list := func(opts images.ListOptsBuilder) []images.Image {
		t.Helper()
		pages, err := images.List(client, opts).AllPages(ctx)
		require.NoError(t, err)
		result, err := images.ExtractImages(pages)
		require.NoError(t, err)
		return result
	}
	baseline := get()
	require.Equal(t, "fixture-image", baseline.Name)
	require.Equal(t, images.ImageStatusActive, baseline.Status)
	assert.Equal(t, routerProjectID(t, provider), baseline.Owner)
	assert.Equal(t, images.ImageVisibilityPrivate, baseline.Visibility)
	assert.Equal(t, []images.Image{*baseline}, list(nil))
	assert.Equal(t, []images.Image{*baseline}, list(images.ListOpts{Name: baseline.Name}))
	assert.Equal(t, []images.Image{*baseline}, list(images.ListOpts{Status: images.ImageStatusActive}))
	active := images.ListOpts{Name: baseline.Name, Status: images.ImageStatusActive}
	inactive := images.ListOpts{Name: baseline.Name, Status: images.ImageStatusDeactivated}
	assert.Equal(t, []images.Image{*baseline}, list(active))
	assert.Empty(t, list(inactive))
	assert.Empty(t, list(images.ListOpts{Name: "fixture", Status: images.ImageStatusActive}))
	assert.Empty(t, list(images.ListOpts{Status: "not-a-modeled-status"}))

	control := "/_sandstack/image/images/" + fixtureImageID + "/deactivated"
	admin := map[string]string{"X-SandStack-Admin-Token": adminToken}
	status, _, body := request(t, instance, http.MethodPut, control, "", admin)
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	deactivated := get()
	require.Equal(t, images.ImageStatusDeactivated, deactivated.Status)
	assert.Equal(t, baseline.Owner, deactivated.Owner)
	assert.Equal(t, images.ImageVisibilityPrivate, deactivated.Visibility)
	assert.Empty(t, list(active))
	assert.Equal(t, []images.Image{*deactivated}, list(inactive))
	assert.Equal(t, []images.Image{*deactivated}, list(images.ListOpts{Name: baseline.Name}))

	status, _, body = request(t, instance, http.MethodDelete, control, "", admin)
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	reactivated := get()
	require.Equal(t, images.ImageStatusActive, reactivated.Status)
	assert.Equal(t, []images.Image{*reactivated}, list(active))
	assert.Empty(t, list(inactive))
}

func TestImageListFiltersWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	status, headers, body := request(t, instance, http.MethodGet, imagesPath+"/"+fixtureImageID, "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	detail := string(body)

	// https://docs.openstack.org/api-ref/image/v2/#list-images
	for _, test := range []struct {
		name  string
		query url.Values
		match bool
	}{
		{name: "unfiltered", query: url.Values{}, match: true},
		{name: "plain equality", query: url.Values{"name": {"fixture-image"}, "status": {"active"}}, match: true},
		{name: "explicit equality", query: url.Values{"name": {"eq:fixture-image"}, "status": {"eq:active"}}, match: true},
		{name: "wrong case", query: url.Values{"name": {"FIXTURE-IMAGE"}}},
		{name: "partial name", query: url.Values{"name": {"fixture"}}},
		{name: "unknown scalar status", query: url.Values{"status": {"unmodeled"}}},
		{name: "different status", query: url.Values{"name": {"fixture-image"}, "status": {"deactivated"}}},
		{name: "encoded name", query: url.Values{"name": {"eq:missing + & image"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := imagesPath
			if query := test.query.Encode(); query != "" {
				path += "?" + query
			}
			status, headers, body := request(t, instance, http.MethodGet, path, "", auth)
			require.Equal(t, http.StatusOK, status, "%s", body)
			requireJSON(t, headers)
			var fields map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(body, &fields))
			assert.Len(t, fields, 3)
			assert.NotContains(t, fields, "next")
			assert.JSONEq(t, `"/image/v2/schemas/images"`, string(fields["schema"]))
			var first string
			require.NoError(t, json.Unmarshal(fields["first"], &first))
			u, err := url.Parse(first)
			require.NoError(t, err)
			assert.False(t, u.IsAbs())
			assert.Equal(t, imagesPath, u.Path)
			assert.Equal(t, test.query, u.Query())
			if test.match {
				var items []json.RawMessage
				require.NoError(t, json.Unmarshal(fields["images"], &items))
				require.Len(t, items, 1)
				assert.JSONEq(t, detail, string(items[0]))
			} else {
				assert.JSONEq(t, "[]", string(fields["images"]))
			}
		})
	}

	status, headers, body = request(t, instance, http.MethodGet, imagesPath+"?name=fixture-image", "", nil)
	require.Equal(t, http.StatusUnauthorized, status, "%s", body)
	assert.Equal(t, "Keystone", headers.Get("WWW-Authenticate"))
	for _, rejected := range []string{
		"name=fixture-image&name=fixture-image",
		"status=active&status=deactivated",
		"name=in%3Afixture-image%2Cother",
		"status=banana%3Aactive",
		"name=%zz",
		"owner=another-project",
		"limit=1",
	} {
		status, _, body := request(t, instance, http.MethodGet, imagesPath+"?"+rejected, "", auth)
		assert.Equal(t, http.StatusBadRequest, status, "%s: %s", rejected, body)
	}
}
