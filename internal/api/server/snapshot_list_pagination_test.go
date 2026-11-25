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
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/snapshots"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGophercloudPaginatesSnapshotsAcrossMetadataAndStateChanges(t *testing.T) {
	instance := startServer(t, "")
	client, fixture := createSnapshotPagingFixtures(t, instance, authenticate(t, instance))
	list := func(opts snapshots.ListOpts) []snapshots.Snapshot {
		t.Helper()
		pages, err := snapshots.ListDetail(client, opts).AllPages(t.Context())
		require.NoError(t, err)
		result, err := snapshots.ExtractSnapshots(pages)
		require.NoError(t, err)
		return result
	}
	name := fixture[0].Name
	available := snapshots.ListOpts{Name: name, Status: "available", Limit: 2, Sort: "id:asc"}
	want := []snapshots.Snapshot{fixture[0], fixture[2], fixture[3], fixture[4]}
	assert.Equal(t, fixture, list(snapshots.ListOpts{Name: name}))
	assert.Equal(t, want, list(snapshots.ListOpts{Status: "available"}))
	assert.Empty(t, list(snapshots.ListOpts{Name: "snap"}))
	assert.Equal(t, want, list(available))
	available.Marker = fixture[1].ID
	assert.Equal(t, want[1:], list(available))
	available.Marker = ""

	newName := "renamed-snapshot"
	_, err := snapshots.Update(t.Context(), client, fixture[0].ID, snapshots.UpdateOpts{Name: &newName}).Extract()
	require.NoError(t, err)
	assert.Equal(t, want[1:], list(available))
	renamed, err := snapshots.Get(t.Context(), client, fixture[0].ID).Extract()
	require.NoError(t, err)
	assert.Equal(t, []snapshots.Snapshot{*renamed}, list(snapshots.ListOpts{Name: newName, Status: "available"}))

	status, _, body := request(t, instance, http.MethodPut, snapshotControlPath(fixture[1].ID, "complete-create"), "",
		map[string]string{"X-SandStack-Admin-Token": adminToken})
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	completed, err := snapshots.Get(t.Context(), client, fixture[1].ID).Extract()
	require.NoError(t, err)
	require.Equal(t, "available", completed.Status)
	assert.Equal(t, []snapshots.Snapshot{*completed, fixture[2], fixture[3], fixture[4]}, list(available))
}

func TestDetailedSnapshotListPaginationWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	_, fixture := createSnapshotPagingFixtures(t, instance, provider)
	collection := volumePath + "/" + volumeProjectID(t, provider) + "/snapshots"
	path := collection + "/detail"
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	query := url.Values{"name": {fixture[0].Name}, "status": {"available"}, "limit": {"2"}, "sort": {"id:asc"}}
	type item struct {
		ID       string          `json:"id"`
		Name     json.RawMessage `json:"name"`
		Status   string          `json:"status"`
		VolumeID string          `json:"volume_id"`
		Progress string          `json:"os-extended-snapshot-attributes:progress"`
	}
	type page struct {
		Items []item             `json:"snapshots"`
		Links []gophercloud.Link `json:"snapshots_links"`
	}
	read := func(query string, want ...string) page {
		t.Helper()
		status, headers, body := request(t, instance, http.MethodGet, path+"?"+query, "", auth)
		require.Equal(t, http.StatusOK, status, "%s", body)
		requireJSON(t, headers)
		assert.Equal(t, "volume 3.0", headers.Get("OpenStack-API-Version"))
		var result page
		require.NoError(t, json.Unmarshal(body, &result))
		require.NotNil(t, result.Items)
		ids := make([]string, 0, len(result.Items))
		for _, snapshot := range result.Items {
			ids = append(ids, snapshot.ID)
			assert.Equal(t, fixtureVolumeID, snapshot.VolumeID)
		}
		assert.Equal(t, append([]string{}, want...), ids)
		if len(result.Links) == 0 {
			var fields map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(body, &fields))
			assert.NotContains(t, fields, "snapshots_links")
		}
		return result
	}
	next := func(current page, marker string) *url.URL {
		t.Helper()
		require.Len(t, current.Links, 1)
		assert.Equal(t, "next", current.Links[0].Rel)
		link, err := url.Parse(current.Links[0].Href)
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

	// https://docs.openstack.org/api-ref/block-storage/v3/#list-snapshots-with-details
	first := read(query.Encode(), a, b)
	require.NotEmpty(t, first.Items)
	assert.Equal(t, "available", first.Items[0].Status)
	assert.Equal(t, "100%", first.Items[0].Progress)
	last := read(next(first, b).RawQuery, c, d)
	assert.Empty(t, read(next(last, d).RawQuery).Links)
	query.Set("marker", fixture[1].ID)
	current := read(query.Encode(), b, c)
	assert.Empty(t, read(next(current, c).RawQuery, d).Links)
	query.Del("marker")
	query.Set("limit", "0")
	assert.Empty(t, read(query.Encode()).Links)
	query.Del("limit")
	query.Del("sort")
	assert.Empty(t, read(query.Encode(), a, b, c, d).Links)
	assert.Empty(t, read("status=invalid").Links)

	for _, revision := range []struct {
		id   string
		name string
	}{
		{a, `""`},
		{b, "null"},
	} {
		status, headers, body := request(t, instance, http.MethodPut, collection+"/"+revision.id,
			`{"snapshot":{"name":`+revision.name+`}}`, auth)
		require.Equal(t, http.StatusOK, status, "%s", body)
		requireJSON(t, headers)
	}
	empty := read("name=&status=available", a)
	require.Len(t, empty.Items, 1)
	assert.JSONEq(t, `""`, string(empty.Items[0].Name))
	assert.Empty(t, empty.Links)

	for _, rejected := range []struct {
		query  string
		status int
		kind   string
	}{
		{"limit=2&marker=00000000-0000-0000-0000-000000000000", http.StatusNotFound, "itemNotFound"},
		{"marker=", http.StatusNotFound, "itemNotFound"},
		{"name=one&name=two", http.StatusBadRequest, "badRequest"},
		{"limit=-1", http.StatusBadRequest, "badRequest"},
		{"limit=1&limit=1", http.StatusBadRequest, "badRequest"},
		{"sort=id%3Adesc", http.StatusBadRequest, "badRequest"},
		{"offset=1", http.StatusBadRequest, "badRequest"},
	} {
		status, headers, body := request(t, instance, http.MethodGet, path+"?"+rejected.query, "", auth)
		require.Equal(t, rejected.status, status, "%s: %s", rejected.query, body)
		requireJSON(t, headers)
		var response map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(body, &response))
		assert.Contains(t, response, rejected.kind)
	}
}

func createSnapshotPagingFixtures(
	t *testing.T, instance *httptest.Server, provider *gophercloud.ProviderClient,
) (*gophercloud.ServiceClient, []snapshots.Snapshot) {
	t.Helper()
	client, err := openstack.NewBlockStorageV3(provider, gophercloud.EndpointOpts{
		Region: "RegionOne", Availability: gophercloud.AvailabilityPublic,
	})
	require.NoError(t, err)
	ctx := t.Context()
	fixture := make([]snapshots.Snapshot, 0, 5)
	for range 5 {
		snapshot, err := snapshots.Create(ctx, client, snapshots.CreateOpts{
			VolumeID: fixtureVolumeID, Name: "snapshot + & page",
		}).Extract()
		require.NoError(t, err)
		require.NotEmpty(t, snapshot.ID)
		fixture = append(fixture, *snapshot)
	}
	slices.SortFunc(fixture, func(a, b snapshots.Snapshot) int { return strings.Compare(a.ID, b.ID) })
	admin := map[string]string{"X-SandStack-Admin-Token": adminToken}
	for i := range fixture {
		if i != 1 {
			status, _, body := request(t, instance, http.MethodPut,
				snapshotControlPath(fixture[i].ID, "complete-create"), "", admin)
			require.Equal(t, http.StatusNoContent, status, "%s", body)
		}
		snapshot, err := snapshots.Get(ctx, client, fixture[i].ID).Extract()
		require.NoError(t, err)
		fixture[i] = *snapshot
	}
	return client, fixture
}
