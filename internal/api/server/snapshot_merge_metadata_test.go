package server_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/snapshots"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGophercloudMergesSnapshotMetadataWithoutReplacingOtherKeys(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	client, err := openstack.NewBlockStorageV3(provider, gophercloud.EndpointOpts{
		Region: "RegionOne", Availability: gophercloud.AvailabilityPublic,
	})
	require.NoError(t, err)
	ctx := t.Context()
	created, err := snapshots.Create(ctx, client, snapshots.CreateOpts{
		VolumeID: fixtureVolumeID, Name: "metadata-merge",
		Metadata: map[string]string{"source/key": "left intact", "keep": "untouched", "revise": "old"},
	}).Extract()
	require.NoError(t, err)
	require.NotEmpty(t, created.ID)
	status, _, body := request(t, instance, http.MethodPut, snapshotControlPath(created.ID, "complete-create"), "",
		map[string]string{"X-SandStack-Admin-Token": adminToken})
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	baseline, err := snapshots.Get(ctx, client, created.ID).Extract()
	require.NoError(t, err)
	require.Equal(t, "available", baseline.Status)

	metadataURL := client.ServiceURL("snapshots", created.ID, "metadata")
	type envelope struct {
		Metadata map[string]string `json:"metadata"`
	}
	var updated envelope
	resp, err := client.Post(ctx, metadataURL, envelope{Metadata: map[string]string{"revise": "new", "added": ""}},
		&updated, &gophercloud.RequestOpts{OkCodes: []int{http.StatusOK}})

	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	requireJSON(t, resp.Header)
	want := *baseline
	want.Metadata = map[string]string{"source/key": "left intact", "keep": "untouched", "revise": "new", "added": ""}
	assert.Equal(t, want.Metadata, updated.Metadata)
	var current envelope
	_, err = client.Get(ctx, metadataURL, &current, nil)
	require.NoError(t, err)
	assert.Equal(t, updated, current)
	detail, err := snapshots.Get(ctx, client, created.ID).Extract()
	require.NoError(t, err)
	assert.Equal(t, &want, detail)
	pages, err := snapshots.ListDetail(client, nil).AllPages(ctx)
	require.NoError(t, err)
	items, err := snapshots.ExtractSnapshots(pages)
	require.NoError(t, err)
	assert.Equal(t, []snapshots.Snapshot{want}, items)
}

func TestSnapshotMergeMetadataWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	collection := volumePath + "/" + volumeProjectID(t, provider) + "/snapshots"
	auth := map[string]string{"X-Auth-Token": provider.Token(), "OpenStack-API-Version": "volume 3.0"}
	status, _, body := request(t, instance, http.MethodPost, collection,
		`{"snapshot":{"volume_id":"`+fixtureVolumeID+`","metadata":{"keep":"old","ref/key":"retained"}}}`, auth)
	require.Equal(t, http.StatusAccepted, status, "%s", body)
	var created struct {
		Snapshot wireSnapshot `json:"snapshot"`
	}
	require.NoError(t, json.Unmarshal(body, &created))
	require.NotEmpty(t, created.Snapshot.ID)
	metadata := collection + "/" + created.Snapshot.ID + "/metadata"

	// https://docs.openstack.org/api-ref/block-storage/v3/#create-a-snapshot-s-metadata
	const patch = `{"metadata":{"added":"value"}}`
	const expected = `{"metadata":{"keep":"old","ref/key":"retained","added":"value"}}`
	status, headers, body := request(t, instance, http.MethodPost, metadata, patch, auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	assert.Equal(t, "volume 3.0", headers.Get("OpenStack-API-Version"))
	assert.JSONEq(t, expected, string(body))
	status, headers, body = request(t, instance, http.MethodPost, metadata, `{"metadata":{}}`, auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	assert.JSONEq(t, expected, string(body))

	for _, invalid := range []string{
		`{"metadata":{"ref/key":"must not change"}}`,
		`{"metadata":{"keep":null}}`,
		`{"metadata":{"keep":12}}`,
		`{"metadata":{"keep":"one","keep":"two"}}`,
		`{"metadata":{"keep":"must not change"},"extra":"unsupported"}`,
		`{"metadata":null}`,
	} {
		status, headers, body := request(t, instance, http.MethodPost, metadata, invalid, auth)
		require.Equal(t, http.StatusBadRequest, status, "%s: %s", invalid, body)
		requireJSON(t, headers)
	}
	status, _, body = request(t, instance, http.MethodPost, metadata, patch, nil)
	require.Equal(t, http.StatusUnauthorized, status, "%s", body)
	const missing = "00000000-0000-0000-0000-000000000000"
	status, headers, body = request(t, instance, http.MethodPost, collection+"/"+missing+"/metadata", patch, auth)
	require.Equal(t, http.StatusNotFound, status, "%s", body)
	requireJSON(t, headers)
	var fault map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(body, &fault))
	assert.Contains(t, fault, "itemNotFound")
	status, headers, body = request(t, instance, http.MethodGet, metadata, "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	assert.JSONEq(t, expected, string(body))
}
