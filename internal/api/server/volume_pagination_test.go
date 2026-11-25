package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/volumes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const missingVolumeMarker = "00000000-0000-0000-0000-000000000000"

func TestGophercloudPaginatesDetailedVolumes(t *testing.T) {
	instance := startServer(t, "")
	client, fixture := createVolumePagingFixtures(t, instance, authenticate(t, instance))
	list := func(opts volumes.ListOpts) []volumes.Volume {
		t.Helper()
		pages, err := volumes.List(client, opts).AllPages(t.Context())
		require.NoError(t, err)
		result, err := volumes.ExtractVolumes(pages)
		require.NoError(t, err)
		return result
	}
	opts := volumes.ListOpts{Name: fixture[0].Name, Status: "available", Limit: 2, Sort: "id:asc"}
	want := []volumes.Volume{fixture[0], fixture[2], fixture[3], fixture[4]}
	assert.Equal(t, want, list(opts))
	opts.Sort = ""
	assert.Equal(t, want, list(opts), "fixture default order is ID ascending")
	opts.Sort = "id:asc"
	opts.Marker = fixture[1].ID
	assert.Equal(t, want[1:], list(opts), "a marker can be an existing volume excluded by filters")
	opts.Marker = fixture[4].ID
	assert.Empty(t, list(opts))
	opts.Limit = 0
	opts.Marker = ""
	assert.Equal(t, want, list(opts), "the SDK omits a zero-valued limit")
}

func TestDetailedVolumePaginationWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	_, fixture := createVolumePagingFixtures(t, instance, provider)
	path := volumePath + "/" + volumeProjectID(t, provider) + "/volumes/detail"
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	query := url.Values{
		"name": {fixture[0].Name}, "status": {"available"}, "limit": {"2"}, "sort": {"id:asc"},
	}
	read := func(query string, want ...string) []gophercloud.Link {
		t.Helper()
		status, headers, body := request(t, instance, http.MethodGet, path+"?"+query, "", auth)
		require.Equal(t, http.StatusOK, status, "%s", body)
		requireJSON(t, headers)
		assert.Equal(t, "volume 3.0", headers.Get("OpenStack-API-Version"))
		var page struct {
			Volumes []wireVolume       `json:"volumes"`
			Links   []gophercloud.Link `json:"volumes_links"`
		}
		require.NoError(t, json.Unmarshal(body, &page))
		require.NotNil(t, page.Volumes)
		ids := make([]string, 0, len(page.Volumes))
		for _, volume := range page.Volumes {
			ids = append(ids, volume.ID)
			assert.Equal(t, fixture[0].Name, volume.Name)
			assert.Equal(t, "available", volume.Status)
			assert.Equal(t, 1, volume.Size)
		}
		assert.Equal(t, append([]string{}, want...), ids)
		if len(page.Links) == 0 {
			var fields map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(body, &fields))
			assert.NotContains(t, fields, "volumes_links")
		}
		return page.Links
	}
	next := func(links []gophercloud.Link, marker string) *url.URL {
		t.Helper()
		require.Len(t, links, 1)
		assert.Equal(t, "next", links[0].Rel)
		link, err := url.Parse(links[0].Href)
		require.NoError(t, err)
		assert.True(t, link.IsAbs())
		assert.Equal(t, instance.URL+path, link.Scheme+"://"+link.Host+link.Path)
		expected, err := url.ParseQuery(query.Encode())
		require.NoError(t, err)
		expected.Set("marker", marker)
		assert.Equal(t, expected, link.Query())
		return link
	}
	a, b, c, d := fixture[0].ID, fixture[2].ID, fixture[3].ID, fixture[4].ID

	// https://docs.openstack.org/api-ref/block-storage/v3/#list-accessible-volumes-with-details
	first := read(query.Encode(), a, b)
	second := read(next(first, b).RawQuery, c, d)
	assert.Empty(t, read(next(second, d).RawQuery))
	query.Set("marker", fixture[1].ID)
	page := read(query.Encode(), b, c)
	assert.Empty(t, read(next(page, c).RawQuery, d))
	query.Del("marker")
	query.Set("limit", "0")
	assert.Empty(t, read(query.Encode()))
	query.Set("limit", "1001")
	assert.Empty(t, read(query.Encode(), a, b, c, d))
	query.Del("limit")
	assert.Empty(t, read(query.Encode(), a, b, c, d))

	checkFault := func(query, kind string, want int) {
		t.Helper()
		status, headers, body := request(t, instance, http.MethodGet, path+"?"+query, "", auth)
		require.Equal(t, want, status, "%s: %s", query, body)
		requireJSON(t, headers)
		var fault map[string]struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		}
		require.NoError(t, json.Unmarshal(body, &fault))
		require.Contains(t, fault, kind)
		assert.Equal(t, want, fault[kind].Code)
		assert.NotEmpty(t, fault[kind].Message)
	}
	for _, marker := range []string{missingVolumeMarker, ""} {
		checkFault("limit=2&marker="+marker, "itemNotFound", http.StatusNotFound)
	}
	for _, bad := range []string{
		"limit=-1", "limit=not-a-number", "limit=1&limit=2",
		"marker=" + a + "&marker=" + b,
		"sort=id%3Aasc&sort=id%3Aasc", "sort=id%3Adesc",
	} {
		checkFault(bad, "badRequest", http.StatusBadRequest)
	}
}

func createVolumePagingFixtures(
	t *testing.T, instance *httptest.Server, provider *gophercloud.ProviderClient,
) (*gophercloud.ServiceClient, []volumes.Volume) {
	t.Helper()
	client, err := openstack.NewBlockStorageV3(provider, gophercloud.EndpointOpts{
		Region: "RegionOne", Availability: gophercloud.AvailabilityPublic,
	})
	require.NoError(t, err)
	ctx := t.Context()
	fixture := make([]volumes.Volume, 0, 5)
	for range 5 {
		volume, err := volumes.Create(ctx, client, volumes.CreateOpts{Name: "page + & volume", Size: 1}, nil).Extract()
		require.NoError(t, err)
		require.NotEmpty(t, volume.ID)
		fixture = append(fixture, *volume)
	}
	slices.SortFunc(fixture, func(a, b volumes.Volume) int { return strings.Compare(a.ID, b.ID) })
	admin := map[string]string{"X-SandStack-Admin-Token": adminToken}
	for _, i := range []int{0, 2, 3, 4} {
		status, _, body := request(t, instance, http.MethodPut, volumeCompleteCreatePath(fixture[i].ID), "", admin)
		require.Equal(t, http.StatusNoContent, status, "%s", body)
		volume, err := volumes.Get(ctx, client, fixture[i].ID).Extract()
		require.NoError(t, err)
		fixture[i] = *volume
	}
	return client, fixture
}
