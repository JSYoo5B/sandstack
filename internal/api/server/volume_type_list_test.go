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
	performance, err := volumetypes.Get(ctx, service, performanceVolumeTypeID).Extract()
	require.NoError(t, err)
	require.Equal(t, "performance", performance.Name)
	all := []volumetypes.VolumeType{*byID, *performance}

	list := func(opts volumetypes.ListOptsBuilder) []volumetypes.VolumeType {
		t.Helper()
		pages, err := volumetypes.List(service, opts).AllPages(ctx)
		require.NoError(t, err)
		items, err := volumetypes.ExtractVolumeTypes(pages)
		require.NoError(t, err)
		return items
	}
	assert.Equal(t, all, list(nil))
	assert.Equal(t, all, list(volumetypes.ListOpts{
		Name: "nonmatching name", Description: "nonmatching description", IsPublic: volumetypes.VisibilityPrivate,
	}))

	// The linked pager requests an empty third page when the second page fills the limit.
	assert.Equal(t, all, list(volumetypes.ListOpts{
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
	assert.Equal(t, []int{200, 200, 200, 200, 200}, typeListStatuses)
	assert.Equal(t, []volumetypes.VolumeType{*performance},
		list(volumetypes.ListOpts{Limit: 1, Marker: fixtureVolumeTypeID}))
	assert.Empty(t, list(volumetypes.ListOpts{Limit: 1, Marker: performanceVolumeTypeID}))
}

func TestCinderVolumeTypeListWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	path := volumePath + "/" + volumeProjectID(t, provider) + "/types"
	auth := map[string]string{"X-Auth-Token": provider.Token(), "OpenStack-API-Version": "volume 3.0"}
	const standardType = `{
        "id":"` + fixtureVolumeTypeID + `","name":"standard",
        "description":"Standard SandStack volume type","is_public":true,
        "os-volume-type-access:is_public":true,"extra_specs":{}
    }`
	const performanceType = `{
        "id":"` + performanceVolumeTypeID + `","name":"performance",
        "description":"Performance SandStack volume type","is_public":true,
        "os-volume-type-access:is_public":true,"extra_specs":{}
    }`
	read := func(query, wantTypes string) map[string]json.RawMessage {
		t.Helper()
		status, headers, body := request(t, instance, http.MethodGet, path+"?"+query, "", auth)
		require.Equal(t, http.StatusOK, status, "%s: %s", query, body)
		requireJSON(t, headers)
		assert.Equal(t, "volume 3.0", headers.Get("OpenStack-API-Version"))
		var page map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(body, &page))
		assert.JSONEq(t, "["+wantTypes+"]", string(page["volume_types"]))
		return page
	}
	nextQuery := func(page map[string]json.RawMessage) url.Values {
		t.Helper()
		require.Len(t, page, 2)
		var links []gophercloud.Link
		require.NoError(t, json.Unmarshal(page["volume_type_links"], &links))
		require.Len(t, links, 1)
		assert.Equal(t, "next", links[0].Rel)
		next, err := url.Parse(links[0].Href)
		require.NoError(t, err)
		assert.True(t, next.IsAbs())
		assert.Equal(t, instance.URL+path, next.Scheme+"://"+next.Host+next.Path)
		return next.Query()
	}

	// https://docs.openstack.org/api-ref/block-storage/v3/#list-all-volume-types
	// https://github.com/openstack/cinder/blob/master/cinder/api/v3/views/types.py#L45-L55
	query := url.Values{
		"is_public": {"false"}, "name": {"name + &"}, "description": {"unknown"}, "limit": {"1"},
	}
	first := read(query.Encode(), standardType)
	query.Set("marker", fixtureVolumeTypeID)
	assert.Equal(t, query, nextQuery(first))
	second := read(query.Encode(), performanceType)
	query.Set("marker", performanceVolumeTypeID)
	assert.Equal(t, query, nextQuery(second))
	last := read(query.Encode(), "")
	assert.Len(t, last, 1)
	full := read("", standardType+","+performanceType)
	assert.Len(t, full, 1)
	empty := read("is_public=None&limit=0", "")
	assert.Len(t, empty, 1)

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
