package server_test

import (
	"net/http"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/snapshots"
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/volumes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGophercloudRestoresCinderSnapshotWithImageProvenance(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	storage, err := openstack.NewBlockStorageV3(provider, gophercloud.EndpointOpts{
		Region: "RegionOne", Availability: gophercloud.AvailabilityPublic,
	})
	require.NoError(t, err)
	ctx := t.Context()
	admin := map[string]string{"X-SandStack-Admin-Token": adminToken}
	control := func(path string) {
		t.Helper()
		status, _, body := request(t, instance, http.MethodPut, path, "", admin)
		require.Equal(t, http.StatusNoContent, status, "%s", body)
	}
	read := func(id string) *volumes.Volume {
		t.Helper()
		volume, err := volumes.Get(ctx, storage, id).Extract()
		require.NoError(t, err)
		return volume
	}
	sourceMetadata := map[string]string{"source/owner": "source team"}
	source, err := volumes.Create(ctx, storage, volumes.CreateOpts{
		Name: "source image volume", Size: 2, ImageID: fixtureImageID, Metadata: sourceMetadata,
	}, nil).Extract()
	require.NoError(t, err)
	require.NotEmpty(t, source.ID)
	control(volumeCompleteCreatePath(source.ID))
	populated := read(source.ID)
	require.Equal(t, "available", populated.Status)
	require.Equal(t, "true", populated.Bootable)
	require.Equal(t, fixtureImageID, populated.VolumeImageMetadata["image_id"])

	snapshotMetadata := map[string]string{"snapshot/owner": "snapshot team"}
	pendingSnapshot, err := snapshots.Create(ctx, storage, snapshots.CreateOpts{
		VolumeID: populated.ID, Name: "source snapshot", Metadata: snapshotMetadata,
	}).Extract()
	require.NoError(t, err)
	require.NotEmpty(t, pendingSnapshot.ID)
	control(snapshotControlPath(pendingSnapshot.ID, "complete-create"))
	sourceSnapshot, err := snapshots.Get(ctx, storage, pendingSnapshot.ID).Extract()
	require.NoError(t, err)
	require.Equal(t, "available", sourceSnapshot.Status)
	require.Equal(t, populated.Size, sourceSnapshot.Size)
	control("/_sandstack/volume/volumes/" + populated.ID + "/unavailable-detail")

	// https://docs.openstack.org/api-ref/block-storage/v3/#create-a-volume
	// https://github.com/openstack/cinder/blob/10.0.0/cinder/volume/flows/manager/create_volume.py#L416-L442
	restoreMetadata := map[string]string{"restore/owner": "restore team"}
	restore := func(name string) *volumes.Volume {
		t.Helper()
		restoring, err := volumes.Create(ctx, storage, volumes.CreateOpts{
			Name: name, SnapshotID: sourceSnapshot.ID, Metadata: restoreMetadata,
		}, nil).Extract()
		require.NoError(t, err)
		require.NotEmpty(t, restoring.ID)
		assert.Equal(t, "creating", restoring.Status)
		assert.Equal(t, "false", restoring.Bootable)
		assert.Nil(t, restoring.VolumeImageMetadata)
		assert.Equal(t, sourceSnapshot.ID, restoring.SnapshotID)
		assert.Equal(t, populated.Size, restoring.Size)
		assert.Equal(t, restoreMetadata, restoring.Metadata)
		assert.Equal(t, restoring, read(restoring.ID))
		return restoring
	}

	const restoredName = "restore with image provenance"
	restoring := restore(restoredName)
	control(volumeCompleteCreatePath(restoring.ID))
	completed := read(restoring.ID)
	assert.Equal(t, "available", completed.Status)
	assert.Equal(t, "true", completed.Bootable)
	assert.Equal(t, populated.VolumeImageMetadata, completed.VolumeImageMetadata)
	assert.Equal(t, restoreMetadata, completed.Metadata)
	assert.Equal(t, sourceSnapshot.ID, completed.SnapshotID)
	pages, err := volumes.List(storage, volumes.ListOpts{Name: restoredName}).AllPages(ctx)
	require.NoError(t, err)
	items, err := volumes.ExtractVolumes(pages)
	require.NoError(t, err)
	assert.Equal(t, []volumes.Volume{*completed}, items)

	second := restore("restore after source bootable is cleared")
	require.NoError(t, volumes.SetBootable(ctx, storage, populated.ID, volumes.BootableOpts{Bootable: false}).ExtractErr())
	control(volumeCompleteCreatePath(second.ID))
	unbootable := read(second.ID)
	assert.Equal(t, "available", unbootable.Status)
	assert.Equal(t, "false", unbootable.Bootable)
	assert.Nil(t, unbootable.VolumeImageMetadata)
	assert.Equal(t, restoreMetadata, unbootable.Metadata)
	assert.Equal(t, completed, read(completed.ID))

	_, err = volumes.Get(ctx, storage, populated.ID).Extract()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusServiceUnavailable), "source detail outage remains pending: %v", err)
	assert.Equal(t, sourceMetadata, read(populated.ID).Metadata)
	snapshotAfter, err := snapshots.Get(ctx, storage, sourceSnapshot.ID).Extract()
	require.NoError(t, err)
	assert.Equal(t, sourceSnapshot, snapshotAfter)
	assert.Equal(t, snapshotMetadata, snapshotAfter.Metadata)

	lostSnapshot := restore("restore whose snapshot is removed")
	require.NoError(t, snapshots.Delete(ctx, storage, sourceSnapshot.ID).ExtractErr())
	status, _, body := request(t, instance, http.MethodPut, volumeCompleteCreatePath(lostSnapshot.ID), "", admin)
	require.Equal(t, http.StatusConflict, status, "%s", body)
	failed := read(lostSnapshot.ID)
	assert.Equal(t, "error", failed.Status)
	assert.Equal(t, "false", failed.Bootable)
	assert.Nil(t, failed.VolumeImageMetadata)
	assert.Equal(t, sourceSnapshot.ID, failed.SnapshotID)
	assert.Equal(t, restoreMetadata, failed.Metadata)
}
