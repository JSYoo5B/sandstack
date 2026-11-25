package server_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/snapshots"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGophercloudReplacesMetadataOnCreatingAndAvailableSnapshots(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	client, err := openstack.NewBlockStorageV3(provider, gophercloud.EndpointOpts{
		Region: "RegionOne", Availability: gophercloud.AvailabilityPublic,
	})
	require.NoError(t, err)
	ctx := t.Context()
	created, err := snapshots.Create(ctx, client, snapshots.CreateOpts{
		VolumeID: fixtureVolumeID, Name: "metadata-snapshot",
	}).Extract()
	require.NoError(t, err)
	require.NotEmpty(t, created.ID)
	metadataURL := client.ServiceURL("snapshots", created.ID, "metadata")
	read := func() *snapshots.Snapshot {
		t.Helper()
		snapshot, err := snapshots.Get(ctx, client, created.ID).Extract()
		require.NoError(t, err)
		return snapshot
	}
	readMetadata := func() map[string]string {
		t.Helper()
		var response struct {
			Metadata map[string]string `json:"metadata"`
		}
		result, err := client.Get(ctx, metadataURL, &response, nil)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, result.StatusCode)
		requireJSON(t, result.Header)
		require.NotNil(t, response.Metadata)
		return response.Metadata
	}
	check := func(want *snapshots.Snapshot) {
		t.Helper()
		assert.Equal(t, want.Metadata, readMetadata())
		assert.Equal(t, want, read())
		pages, err := snapshots.ListDetail(client, nil).AllPages(ctx)
		require.NoError(t, err)
		items, err := snapshots.ExtractSnapshots(pages)
		require.NoError(t, err)
		assert.Equal(t, []snapshots.Snapshot{*want}, items)
	}
	replace := func(fields map[string]any) {
		t.Helper()
		updated, err := snapshots.UpdateMetadata(ctx, client, created.ID, snapshots.UpdateMetadataOpts{
			Metadata: fields,
		}).ExtractMetadata()
		require.NoError(t, err)
		assert.Equal(t, fields, updated)
	}
	pending := read()
	require.Equal(t, "creating", pending.Status)
	require.True(t, pending.UpdatedAt.IsZero())
	check(pending)
	replace(map[string]any{"keep": "old", "remove": "old-value"})
	want := *pending
	want.Metadata = map[string]string{"keep": "old", "remove": "old-value"}
	check(&want)

	status, _, body := request(t, instance, http.MethodPut, snapshotControlPath(created.ID, "complete-create"), "",
		map[string]string{"X-SandStack-Admin-Token": adminToken})
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	available := read()
	require.Equal(t, "available", available.Status)
	require.False(t, available.UpdatedAt.IsZero())
	assert.Equal(t, want.Metadata, available.Metadata)
	replace(map[string]any{"keep": "new", "added": "add-value"})
	want = *available
	want.Metadata = map[string]string{"keep": "new", "added": "add-value"}
	check(&want)
}

func TestSnapshotMetadataReplacementWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	project := volumeProjectID(t, provider)
	collection := volumePath + "/" + project + "/snapshots"
	auth := map[string]string{"X-Auth-Token": provider.Token(), "OpenStack-API-Version": "volume 3.0"}
	status, headers, body := request(t, instance, http.MethodPost, collection,
		`{"snapshot":{"volume_id":"`+fixtureVolumeID+`","name":"wire-metadata"}}`, auth)
	require.Equal(t, http.StatusAccepted, status, "%s", body)
	requireJSON(t, headers)
	var created struct {
		Snapshot wireSnapshot `json:"snapshot"`
	}
	require.NoError(t, json.Unmarshal(body, &created))
	require.NotEmpty(t, created.Snapshot.ID)
	detail := collection + "/" + created.Snapshot.ID
	metadata := detail + "/metadata"
	check := func(method, path, payload, expected string) {
		t.Helper()
		status, headers, body := request(t, instance, method, path, payload, auth)
		require.Equal(t, http.StatusOK, status, "%s", body)
		requireJSON(t, headers)
		assert.Equal(t, "volume 3.0", headers.Get("OpenStack-API-Version"))
		assert.JSONEq(t, expected, string(body))
	}
	baselineStatus, _, baselineBody := request(t, instance, http.MethodGet, detail, "", auth)
	require.Equal(t, http.StatusOK, baselineStatus, "%s", baselineBody)

	// https://docs.openstack.org/api-ref/block-storage/v3/#show-a-snapshot-s-metadata
	// https://docs.openstack.org/api-ref/block-storage/v3/#update-a-snapshot-s-metadata
	const empty = `{"metadata":{}}`
	const fields = `{"metadata":{"role":"fixture","preserved":"yes"}}`
	check(http.MethodGet, metadata, "", empty)
	check(http.MethodPut, metadata, fields, fields)
	check(http.MethodGet, metadata, "", fields)

	long := strings.Repeat("a", 256)
	for _, payload := range []string{
		`{}`,
		`{"metadata":null}`,
		`{"metadata":[]}`,
		`{"metadata":"invalid"}`,
		`{"metadata":{"field":null}}`,
		`{"metadata":{"field":10}}`,
		`{"metadata":{"field":{"nested":"invalid"}}}`,
		`{"metadata":{"repeated":"one","repeated":"two"}}`,
		`{"metadata":{},"metadata":{"role":"unexpected"}}`,
		`{"metadata":{"role":"must-not-change"},"extra":true}`,
		fmt.Sprintf(`{"metadata":{%q:"too long"}}`, long),
		fmt.Sprintf(`{"metadata":{"field":%q}}`, long),
	} {
		status, headers, body := request(t, instance, http.MethodPut, metadata, payload, auth)
		require.Equal(t, http.StatusBadRequest, status, "%s: %s", payload, body)
		requireJSON(t, headers)
	}
	check(http.MethodGet, metadata, "", fields)
	check(http.MethodPut, metadata, empty, empty)
	check(http.MethodGet, metadata, "", empty)
	check(http.MethodGet, detail, "", string(baselineBody))

	const missing = "00000000-0000-0000-0000-000000000000"
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		payload := ""
		if method == http.MethodPut {
			payload = fields
		}
		status, headers, body := request(t, instance, method, collection+"/"+missing+"/metadata", payload, auth)
		require.Equal(t, http.StatusNotFound, status, "%s: %s", method, body)
		requireJSON(t, headers)
		var fault map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(body, &fault))
		assert.Contains(t, fault, "itemNotFound")
	}
}
