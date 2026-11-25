package server_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/snapshots"
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/volumes"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/servers"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/volumeattach"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGophercloudForcesSnapshotOfAttachedVolume(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	endpoint := gophercloud.EndpointOpts{Region: "RegionOne", Availability: gophercloud.AvailabilityPublic}
	storage, err := openstack.NewBlockStorageV3(provider, endpoint)
	require.NoError(t, err)
	compute, err := openstack.NewComputeV2(provider, endpoint)
	require.NoError(t, err)
	ctx := t.Context()
	attached, err := volumeattach.Create(ctx, compute, fixtureServerID, volumeattach.CreateOpts{
		VolumeID: fixtureVolumeID,
	}).Extract()
	require.NoError(t, err)
	volume, err := volumes.Get(ctx, storage, fixtureVolumeID).Extract()
	require.NoError(t, err)
	require.Equal(t, "in-use", volume.Status)
	require.Len(t, volume.Attachments, 1)
	server, err := servers.Get(ctx, compute, fixtureServerID).Extract()
	require.NoError(t, err)
	require.Equal(t, []servers.AttachedVolume{{ID: volume.ID}}, server.AttachedVolumes)
	checkSource := func() {
		t.Helper()
		current, err := volumes.Get(ctx, storage, fixtureVolumeID).Extract()
		require.NoError(t, err)
		assert.Equal(t, volume, current)
		association, err := volumeattach.Get(ctx, compute, fixtureServerID, volume.ID).Extract()
		require.NoError(t, err)
		assert.Equal(t, attached, association)
		currentServer, err := servers.Get(ctx, compute, fixtureServerID).Extract()
		require.NoError(t, err)
		assert.Equal(t, server, currentServer)
	}
	list := func() []snapshots.Snapshot {
		t.Helper()
		pages, err := snapshots.ListDetail(storage, nil).AllPages(ctx)
		require.NoError(t, err)
		items, err := snapshots.ExtractSnapshots(pages)
		require.NoError(t, err)
		return items
	}
	opts := snapshots.CreateOpts{VolumeID: volume.ID, Name: "attached-source"}
	_, err = snapshots.Create(ctx, storage, opts).Extract()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusBadRequest), "force omitted: %v", err)
	collection := volumePath + "/" + volumeProjectID(t, provider) + "/snapshots"
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	payload := fmt.Sprintf(`{"snapshot":{"volume_id":%q,"force":false}}`, volume.ID)
	status, headers, body := request(t, instance, http.MethodPost, collection, payload, auth)
	require.Equal(t, http.StatusBadRequest, status, "%s", body)
	requireJSON(t, headers)
	assert.Empty(t, list())
	checkSource()

	opts.Force = true
	pending, err := snapshots.Create(ctx, storage, opts).Extract()

	require.NoError(t, err)
	require.NotEmpty(t, pending.ID)
	assert.Equal(t, volume.ID, pending.VolumeID)
	assert.Equal(t, volume.Size, pending.Size)
	assert.Equal(t, "creating", pending.Status)
	checkSource()

	status, _, body = request(t, instance, http.MethodPut, snapshotControlPath(pending.ID, "complete-create"), "",
		map[string]string{"X-SandStack-Admin-Token": adminToken})
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	complete, err := snapshots.Get(ctx, storage, pending.ID).Extract()
	require.NoError(t, err)
	assert.Equal(t, pending.ID, complete.ID)
	assert.Equal(t, volume.ID, complete.VolumeID)
	assert.Equal(t, volume.Size, complete.Size)
	assert.Equal(t, "available", complete.Status)
	assert.Equal(t, "100%", complete.Progress)
	assert.Equal(t, []snapshots.Snapshot{*complete}, list())
	checkSource()
}

func TestForcedSnapshotCreateWireAndSourceState(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	storage, err := openstack.NewBlockStorageV3(provider, gophercloud.EndpointOpts{
		Region: "RegionOne", Availability: gophercloud.AvailabilityPublic,
	})
	require.NoError(t, err)
	collection := volumePath + "/" + volumeProjectID(t, provider) + "/snapshots"
	auth := map[string]string{"X-Auth-Token": provider.Token(), "OpenStack-API-Version": "volume 3.0"}
	payload := func(id string) string {
		return fmt.Sprintf(`{"snapshot":{"volume_id":%q,"name":"forced-wire","force":true}}`, id)
	}
	ctx := t.Context()
	pendingVolume, err := volumes.Create(ctx, storage, volumes.CreateOpts{Size: 2, Name: "not-yet-ready"}, nil).Extract()
	require.NoError(t, err)
	require.Equal(t, "creating", pendingVolume.Status)
	_, err = snapshots.Create(ctx, storage, snapshots.CreateOpts{VolumeID: pendingVolume.ID, Force: true}).Extract()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusBadRequest), "force cannot bypass creating: %v", err)

	admin := map[string]string{"X-SandStack-Admin-Token": adminToken}
	status, _, body := request(t, instance, http.MethodPut, volumeCompleteCreatePath(pendingVolume.ID), "", admin)
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	status, _, body = request(t, instance, http.MethodPut,
		"/_sandstack/volume/volumes/"+pendingVolume.ID+"/fault", "", admin)
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	failed, err := volumes.Get(ctx, storage, pendingVolume.ID).Extract()
	require.NoError(t, err)
	require.Equal(t, "error", failed.Status)
	status, headers, body := request(t, instance, http.MethodPost, collection, payload(failed.ID), auth)
	require.Equal(t, http.StatusBadRequest, status, "%s", body)
	requireJSON(t, headers)
	status, headers, body = request(t, instance, http.MethodGet, collection+"/detail", "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	assert.JSONEq(t, `{"snapshots":[]}`, string(body))

	// https://docs.openstack.org/api-ref/block-storage/v3/#create-a-snapshot
	status, headers, body = request(t, instance, http.MethodPost, collection, payload(fixtureVolumeID), auth)
	require.Equal(t, http.StatusAccepted, status, "%s", body)
	requireJSON(t, headers)
	assert.Equal(t, "volume 3.0", headers.Get("OpenStack-API-Version"))
	var response struct {
		Snapshot wireSnapshot `json:"snapshot"`
	}
	require.NoError(t, json.Unmarshal(body, &response))
	assert.NotEmpty(t, response.Snapshot.ID)
	assert.Equal(t, fixtureVolumeID, response.Snapshot.VolumeID)
	assert.Equal(t, "forced-wire", response.Snapshot.Name)
	assert.Equal(t, "creating", response.Snapshot.Status)
	assert.Equal(t, 1, response.Snapshot.Size)
	assert.JSONEq(t, "null", string(response.Snapshot.UpdatedAt))
}
