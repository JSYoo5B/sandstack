package server_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/snapshots"
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/volumes"
	"github.com/gophercloud/gophercloud/v2/openstack/image/v2/imagedata"
	"github.com/gophercloud/gophercloud/v2/openstack/image/v2/images"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGophercloudCreatesBootableCinderVolumeFromGlanceImage(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	endpoint := gophercloud.EndpointOpts{Region: "RegionOne", Availability: gophercloud.AvailabilityPublic}
	imageClient, err := openstack.NewImageV2(provider, endpoint)
	require.NoError(t, err)
	storage, err := openstack.NewBlockStorageV3(provider, endpoint)
	require.NoError(t, err)
	ctx := t.Context()
	private := images.ImageVisibilityPrivate
	queued, err := images.Create(ctx, imageClient, images.CreateOpts{
		Name: "Cinder source image", Visibility: &private, ContainerFormat: "bare", DiskFormat: "qcow2",
		MinDisk: 2, MinRAM: 2048,
	}).Extract()
	require.NoError(t, err)
	require.Equal(t, images.ImageStatusQueued, queued.Status)
	require.Equal(t, 2, queued.MinDiskGigabytes)
	require.Equal(t, 2048, queued.MinRAMMegabytes)

	_, err = volumes.Create(ctx, storage, volumes.CreateOpts{Size: 2, ImageID: queued.ID}, nil).Extract()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusBadRequest), "queued source: %v", err)
	payload := []byte{0x51, 0x46, 0x49, 0xfb, 0x00, 0xff}
	require.NoError(t, imagedata.Upload(ctx, imageClient, queued.ID, bytes.NewReader(payload)).ExtractErr())
	source, err := images.Get(ctx, imageClient, queued.ID).Extract()
	require.NoError(t, err)
	require.Equal(t, images.ImageStatusActive, source.Status)
	require.NotEmpty(t, source.Checksum)

	_, err = volumes.Create(ctx, storage, volumes.CreateOpts{Size: 1, ImageID: source.ID}, nil).Extract()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusBadRequest), "undersized destination: %v", err)

	userMetadata := map[string]string{"image_name": "user override", "role": "boot-disk"}
	created, err := volumes.Create(ctx, storage, volumes.CreateOpts{
		Size: 2, ImageID: source.ID, Name: "volume from images API", Metadata: userMetadata,
	}, nil).Extract()
	require.NoError(t, err)
	require.NotEmpty(t, created.ID)
	assert.Equal(t, "creating", created.Status)
	assert.Equal(t, "false", created.Bootable)
	assert.Nil(t, created.VolumeImageMetadata)
	assert.Equal(t, userMetadata, created.Metadata)
	pending, err := volumes.Get(ctx, storage, created.ID).Extract()
	require.NoError(t, err)
	assert.Equal(t, "creating", pending.Status)
	assert.Equal(t, "false", pending.Bootable)
	assert.Nil(t, pending.VolumeImageMetadata)
	sourceDuringCreate, err := images.Get(ctx, imageClient, source.ID).Extract()
	require.NoError(t, err)
	assert.Equal(t, source, sourceDuringCreate)

	status, _, body := request(t, instance, http.MethodPut, volumeCompleteCreatePath(created.ID), "",
		map[string]string{"X-SandStack-Admin-Token": adminToken})
	require.Equal(t, http.StatusNoContent, status, "%s", body)

	available, err := volumes.Get(ctx, storage, created.ID).Extract()
	require.NoError(t, err)
	assert.Equal(t, 2, available.Size)
	assert.Equal(t, "available", available.Status)
	assert.Equal(t, "true", available.Bootable)
	assert.Equal(t, userMetadata, available.Metadata)
	assert.Equal(t, map[string]string{
		"image_id":         source.ID,
		"image_name":       source.Name,
		"container_format": "bare",
		"disk_format":      "qcow2",
		"checksum":         source.Checksum,
		"size":             strconv.Itoa(len(payload)),
		"min_disk":         "2",
		"min_ram":          "2048",
	}, available.VolumeImageMetadata)

	pages, err := volumes.List(storage, volumes.ListOpts{Name: created.Name}).AllPages(ctx)
	require.NoError(t, err)
	listed, err := volumes.ExtractVolumes(pages)
	require.NoError(t, err)
	assert.Equal(t, []volumes.Volume{*available}, listed)
	sourceAfterCreate, err := images.Get(ctx, imageClient, source.ID).Extract()
	require.NoError(t, err)
	assert.Equal(t, source, sourceAfterCreate)
}

func TestCinderVolumeImageSourceWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	base := volumePath + "/" + volumeProjectID(t, provider) + "/volumes"
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	admin := map[string]string{"X-SandStack-Admin-Token": adminToken}
	endpoint := gophercloud.EndpointOpts{Region: "RegionOne", Availability: gophercloud.AvailabilityPublic}
	storage, err := openstack.NewBlockStorageV3(provider, endpoint)
	require.NoError(t, err)
	snapshot, err := snapshots.Create(t.Context(), storage, snapshots.CreateOpts{VolumeID: fixtureVolumeID}).Extract()
	require.NoError(t, err)
	status, _, body := request(t, instance, http.MethodPut, snapshotControlPath(snapshot.ID, "complete-create"), "", admin)
	require.Equal(t, http.StatusNoContent, status, "%s", body)

	// https://docs.openstack.org/api-ref/block-storage/v3/#create-a-volume
	// https://github.com/openstack/cinder/blob/master/cinder/api/v3/volumes.py#L360-L403
	for _, tt := range []struct{ name, fields string }{
		{name: "missing image", fields: `"imageRef":"00000000-0000-0000-0000-000000000000"`},
		{name: "conflicting snapshot source", fields: fmt.Sprintf(`"imageRef":%q,"snapshot_id":%q`, fixtureImageID, snapshot.ID)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			status, headers, body := request(t, instance, http.MethodPost, base, `{"volume":{"size":2,`+tt.fields+`}}`, auth)
			require.Equal(t, http.StatusBadRequest, status, "%s", body)
			requireJSON(t, headers)
		})
	}

	const deactivate = "/_sandstack/image/images/" + fixtureImageID + "/deactivated"
	status, _, body = request(t, instance, http.MethodPut, deactivate, "", admin)
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	alias := fmt.Sprintf(`{"volume":{"size":2,"image_id":%q}}`, fixtureImageID)
	status, _, body = request(t, instance, http.MethodPost, base, alias, auth)
	require.Equal(t, http.StatusBadRequest, status, "%s", body)
	status, _, body = request(t, instance, http.MethodDelete, deactivate, "", admin)
	require.Equal(t, http.StatusNoContent, status, "%s", body)

	status, headers, body := request(t, instance, http.MethodPost, base, alias, auth)
	require.Equal(t, http.StatusAccepted, status, "%s", body)
	requireJSON(t, headers)
	assert.Equal(t, "volume 3.0", headers.Get("OpenStack-API-Version"))
	type response struct {
		Volume map[string]json.RawMessage `json:"volume"`
	}
	var created response
	require.NoError(t, json.Unmarshal(body, &created))
	assert.JSONEq(t, `"false"`, string(created.Volume["bootable"]))
	assert.JSONEq(t, `"creating"`, string(created.Volume["status"]))
	assert.NotContains(t, created.Volume, "volume_image_metadata")

	var id string
	require.NoError(t, json.Unmarshal(created.Volume["id"], &id))
	require.NotEmpty(t, id)
	status, _, body = request(t, instance, http.MethodPut, "/_sandstack/volume/volumes/"+id+"/fault", "", admin)
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	status, headers, body = request(t, instance, http.MethodGet, base+"/"+id, "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	var failed response
	require.NoError(t, json.Unmarshal(body, &failed))
	assert.JSONEq(t, `"error"`, string(failed.Volume["status"]))
	assert.JSONEq(t, `"false"`, string(failed.Volume["bootable"]))
	assert.NotContains(t, failed.Volume, "volume_image_metadata")
}
