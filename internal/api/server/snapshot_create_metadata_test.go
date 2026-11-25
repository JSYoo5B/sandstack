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
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/volumeattach"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGophercloudCreatesSnapshotWithMetadataThroughCompletion(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	endpoint := gophercloud.EndpointOpts{Region: "RegionOne", Availability: gophercloud.AvailabilityPublic}
	storage, err := openstack.NewBlockStorageV3(provider, endpoint)
	require.NoError(t, err)
	compute, err := openstack.NewComputeV2(provider, endpoint)
	require.NoError(t, err)
	ctx := t.Context()
	attachment, err := volumeattach.Create(ctx, compute, fixtureServerID, volumeattach.CreateOpts{
		VolumeID: fixtureVolumeID,
	}).Extract()
	require.NoError(t, err)
	const slashKey = "example/owner"
	fields := map[string]string{slashKey: "fixture team", "empty-value": "", "retention": "short"}

	created, err := snapshots.Create(ctx, storage, snapshots.CreateOpts{
		VolumeID: fixtureVolumeID, Name: "snapshot-with-initial-metadata", Force: true, Metadata: fields,
	}).Extract()

	require.NoError(t, err)
	require.NotEmpty(t, created.ID)
	assert.Equal(t, "creating", created.Status)
	assert.Equal(t, fixtureVolumeID, created.VolumeID)
	assert.Equal(t, fields, created.Metadata)
	read := func() *snapshots.Snapshot {
		t.Helper()
		detail, err := snapshots.Get(ctx, storage, created.ID).Extract()
		require.NoError(t, err)
		return detail
	}
	check := func(expected *snapshots.Snapshot) {
		t.Helper()
		var response struct {
			Metadata map[string]string `json:"metadata"`
		}
		_, err := storage.Get(ctx, storage.ServiceURL("snapshots", created.ID, "metadata"), &response, nil)
		require.NoError(t, err)
		assert.Equal(t, fields, response.Metadata)
		assert.Equal(t, expected, read())
		pages, err := snapshots.ListDetail(storage, nil).AllPages(ctx)
		require.NoError(t, err)
		items, err := snapshots.ExtractSnapshots(pages)
		require.NoError(t, err)
		assert.Equal(t, []snapshots.Snapshot{*expected}, items)
	}
	want := *created
	want.Progress = "0%"
	want.ProjectID = volumeProjectID(t, provider)
	check(&want)

	_, err = snapshots.UpdateMetadata(ctx, storage, created.ID, snapshots.UpdateMetadataOpts{
		Metadata: map[string]any{slashKey: "must not change"},
	}).ExtractMetadata()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusBadRequest), "slash key on metadata PUT: %v", err)
	check(&want)

	status, _, body := request(t, instance, http.MethodPut, snapshotControlPath(created.ID, "complete-create"), "",
		map[string]string{"X-SandStack-Admin-Token": adminToken})
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	completed := read()
	require.False(t, completed.UpdatedAt.IsZero())
	want.Status, want.Progress, want.UpdatedAt = "available", "100%", completed.UpdatedAt
	check(&want)
	stillAttached, err := volumeattach.Get(ctx, compute, fixtureServerID, fixtureVolumeID).Extract()
	require.NoError(t, err)
	assert.Equal(t, attachment, stillAttached)
}

func TestSnapshotCreateMetadataWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	collection := volumePath + "/" + volumeProjectID(t, provider) + "/snapshots"
	auth := map[string]string{"X-Auth-Token": provider.Token(), "OpenStack-API-Version": "volume 3.0"}
	payload := func(metadata string) string {
		inner := fmt.Sprintf(`"volume_id":%q`, fixtureVolumeID)
		if metadata != "" {
			inner += `,"metadata":` + metadata
		}
		return `{"snapshot":{` + inner + `}}`
	}

	// https://docs.openstack.org/api-ref/block-storage/v3/#create-a-snapshot
	long := strings.Repeat("a", 256)
	for _, invalid := range []string{
		`[]`,
		`"invalid"`,
		`{"key":null}`,
		`{"key":true}`,
		`{"":"empty key"}`,
		`{"duplicate":"first","duplicate":"second"}`,
		fmt.Sprintf(`{%q:"long key"}`, long),
		fmt.Sprintf(`{"long-value":%q}`, long),
	} {
		status, headers, body := request(t, instance, http.MethodPost, collection, payload(invalid), auth)
		require.Equal(t, http.StatusBadRequest, status, "%s: %s", invalid, body)
		requireJSON(t, headers)
	}
	status, headers, body := request(t, instance, http.MethodGet, collection+"/detail", "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	assert.JSONEq(t, `{"snapshots":[]}`, string(body))

	for _, metadata := range []string{"", "null", "{}"} {
		status, headers, body := request(t, instance, http.MethodPost, collection, payload(metadata), auth)
		require.Equal(t, http.StatusAccepted, status, "%s: %s", metadata, body)
		requireJSON(t, headers)
		assert.Equal(t, "volume 3.0", headers.Get("OpenStack-API-Version"))
		var response struct {
			Snapshot wireSnapshot `json:"snapshot"`
		}
		require.NoError(t, json.Unmarshal(body, &response))
		require.NotEmpty(t, response.Snapshot.ID)
		assert.Equal(t, fixtureVolumeID, response.Snapshot.VolumeID)
		assert.Equal(t, "creating", response.Snapshot.Status)
		assert.Equal(t, map[string]string{}, response.Snapshot.Metadata)
		status, headers, body = request(t, instance, http.MethodGet, collection+"/"+response.Snapshot.ID+"/metadata", "", auth)
		require.Equal(t, http.StatusOK, status, "%s", body)
		requireJSON(t, headers)
		assert.JSONEq(t, `{"metadata":{}}`, string(body))
	}
}
