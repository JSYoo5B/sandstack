package server_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/volumetypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGophercloudListsCinderVolumeTypes(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	service, err := openstack.NewBlockStorageV3(provider, gophercloud.EndpointOpts{
		Region: "RegionOne", Availability: gophercloud.AvailabilityPublic,
	})
	require.NoError(t, err)
	ctx := t.Context()
	byID, err := volumetypes.Get(ctx, service, fixtureVolumeTypeID).Extract()
	require.NoError(t, err)
	require.Equal(t, "standard", byID.Name)
	require.True(t, byID.IsPublic)

	list := func(opts volumetypes.ListOptsBuilder) []volumetypes.VolumeType {
		t.Helper()
		pages, err := volumetypes.List(service, opts).AllPages(ctx)
		require.NoError(t, err)
		items, err := volumetypes.ExtractVolumeTypes(pages)
		require.NoError(t, err)
		return items
	}
	assert.Equal(t, []volumetypes.VolumeType{*byID}, list(nil))
	assert.Equal(t, []volumetypes.VolumeType{*byID}, list(volumetypes.ListOpts{
		Name: "nonmatching name", Description: "nonmatching description", IsPublic: volumetypes.VisibilityPrivate,
	}))

	// The linked pager requests an empty second page when the first page fills the limit.
	assert.Equal(t, []volumetypes.VolumeType{*byID}, list(volumetypes.ListOpts{
		Limit: 1, IsPublic: volumetypes.VisibilityPublic,
	}))
	status, headers, body := request(t, instance, http.MethodGet, "/_sandstack/requests", "",
		map[string]string{"X-SandStack-Admin-Token": adminToken})
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	var journal struct {
		Requests []struct {
			Method string `json:"method"`
			Route  string `json:"route"`
			Status int    `json:"status"`
		} `json:"requests"`
	}
	require.NoError(t, json.Unmarshal(body, &journal))
	var typeListStatuses []int
	for _, entry := range journal.Requests {
		if entry.Method == http.MethodGet && entry.Route == volumePath+"/{projectID}/types" {
			typeListStatuses = append(typeListStatuses, entry.Status)
		}
	}
	assert.Equal(t, []int{200, 200, 200, 200}, typeListStatuses)
	assert.Empty(t, list(volumetypes.ListOpts{Limit: 1, Marker: fixtureVolumeTypeID}))
}

func TestCinderVolumeTypeListWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	path := volumePath + "/" + volumeProjectID(t, provider) + "/types"
	auth := map[string]string{"X-Auth-Token": provider.Token(), "OpenStack-API-Version": "volume 3.0"}
	const fixtureType = `{
        "id":"` + fixtureVolumeTypeID + `","name":"standard",
        "description":"Standard SandStack volume type","is_public":true,
        "os-volume-type-access:is_public":true,"extra_specs":{}
    }`
	read := func(query string, wantCount int) map[string]json.RawMessage {
		t.Helper()
		status, headers, body := request(t, instance, http.MethodGet, path+"?"+query, "", auth)
		require.Equal(t, http.StatusOK, status, "%s: %s", query, body)
		requireJSON(t, headers)
		assert.Equal(t, "volume 3.0", headers.Get("OpenStack-API-Version"))
		var page map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(body, &page))
		if wantCount == 0 {
			assert.JSONEq(t, `{"volume_types":[]}`, string(body))
		} else {
			assert.JSONEq(t, "["+fixtureType+"]", string(page["volume_types"]))
		}
		return page
	}

	// https://docs.openstack.org/api-ref/block-storage/v3/#list-all-volume-types
	// https://github.com/openstack/cinder/blob/master/cinder/api/v3/views/types.py#L45-L55
	query := url.Values{
		"is_public": {"false"}, "name": {"name + &"}, "description": {"unknown"}, "limit": {"1"},
	}
	first := read(query.Encode(), 1)
	require.Len(t, first, 2)
	var links []gophercloud.Link
	require.NoError(t, json.Unmarshal(first["volume_type_links"], &links))
	require.Len(t, links, 1)
	assert.Equal(t, "next", links[0].Rel)
	next, err := url.Parse(links[0].Href)
	require.NoError(t, err)
	assert.True(t, next.IsAbs())
	assert.Equal(t, instance.URL+path, next.Scheme+"://"+next.Host+next.Path)
	query.Set("marker", fixtureVolumeTypeID)
	assert.Equal(t, query, next.Query())
	read(next.RawQuery, 0)
	read("is_public=None&limit=0", 0)

	for _, invalid := range []string{
		"offset=1", "limit=1&limit=2", "is_public=None&is_public=false",
		"name=a&name=b", "description=a&description=b", "marker=a&marker=b",
	} {
		status, headers, body := request(t, instance, http.MethodGet, path+"?"+invalid, "", auth)
		assert.Equal(t, http.StatusBadRequest, status, "%s: %s", invalid, body)
		requireJSON(t, headers)
	}
	status, _, body := request(t, instance, http.MethodGet, path+"?marker="+missingVolumeMarker, "", auth)
	assert.Equal(t, http.StatusNotFound, status, "%s", body)
	status, _, body = request(t, instance, http.MethodGet, volumePath+"/"+missingVolumeMarker+"/types", "", auth)
	assert.Equal(t, http.StatusBadRequest, status, "%s", body)
	status, _, body = request(t, instance, http.MethodGet, path, "", nil)
	assert.Equal(t, http.StatusUnauthorized, status, "%s", body)
}
