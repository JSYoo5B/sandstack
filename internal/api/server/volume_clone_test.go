package server_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/volumes"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/volumeattach"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGophercloudClonesCinderVolumeWithoutCopyingAttachments(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	endpoint := gophercloud.EndpointOpts{Region: "RegionOne", Availability: gophercloud.AvailabilityPublic}
	storage, err := openstack.NewBlockStorageV3(provider, endpoint)
	require.NoError(t, err)
	compute, err := openstack.NewComputeV2(provider, endpoint)
	require.NoError(t, err)
	ctx := t.Context()
	admin := map[string]string{"X-SandStack-Admin-Token": adminToken}
	source, err := volumes.Create(ctx, storage, volumes.CreateOpts{
		Name: "bootable clone source", Size: 2, ImageID: fixtureImageID,
		Metadata: map[string]string{"source-only": "keep on source"},
	}, nil).Extract()
	require.NoError(t, err)
	control := func(path string) {
		t.Helper()
		status, _, body := request(t, instance, http.MethodPut, path, "", admin)
		require.Equal(t, http.StatusNoContent, status, "%s", body)
	}
	control(volumeCompleteCreatePath(source.ID))
	ready, err := volumes.Get(ctx, storage, source.ID).Extract()
	require.NoError(t, err)
	require.Equal(t, "true", ready.Bootable)
	require.Equal(t, fixtureImageID, ready.VolumeImageMetadata["image_id"])
	require.Equal(t, "available", ready.Status)

	sameSize, err := volumes.Create(ctx, storage, volumes.CreateOpts{
		Name: "equal size clone", Size: ready.Size, SourceVolID: ready.ID,
	}, nil).Extract()
	require.NoError(t, err)
	assert.Equal(t, ready.ID, sameSize.SourceVolID)
	assert.Equal(t, ready.Size, sameSize.Size)
	assert.Equal(t, map[string]string{}, sameSize.Metadata)
	larger, err := volumes.Create(ctx, storage, volumes.CreateOpts{
		Name: "larger clone", Size: ready.Size + 1, SourceVolID: ready.ID,
	}, nil).Extract()
	require.NoError(t, err)
	assert.Equal(t, ready.ID, larger.SourceVolID)
	assert.Equal(t, ready.Size+1, larger.Size)

	_, err = volumes.Create(ctx, storage, volumes.CreateOpts{Size: ready.Size - 1, SourceVolID: ready.ID}, nil).Extract()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusBadRequest), "undersized clone: %v", err)
	attachment, err := volumeattach.Create(ctx, compute, fixtureServerID, volumeattach.CreateOpts{
		VolumeID: ready.ID,
	}).Extract()
	require.NoError(t, err)
	attached, err := volumes.Get(ctx, storage, ready.ID).Extract()
	require.NoError(t, err)
	require.Equal(t, "in-use", attached.Status)
	require.Len(t, attached.Attachments, 1)
	control("/_sandstack/volume/volumes/" + ready.ID + "/unavailable-detail")

	const name = "clone of attached bootable volume"
	fields := map[string]string{"clone-only": "set on clone"}
	clone, err := volumes.Create(ctx, storage, volumes.CreateOpts{
		Name: name, SourceVolID: ready.ID, Metadata: fields,
	}, nil).Extract()

	require.NoError(t, err)
	require.NotEmpty(t, clone.ID)
	assert.NotEqual(t, ready.ID, clone.ID)
	assert.Equal(t, ready.ID, clone.SourceVolID)
	assert.Equal(t, ready.Size, clone.Size)
	assert.Equal(t, "creating", clone.Status)
	assert.Equal(t, "true", clone.Bootable)
	assert.Nil(t, clone.VolumeImageMetadata)
	assert.Empty(t, clone.Attachments)
	assert.Equal(t, fields, clone.Metadata)
	pending, err := volumes.Get(ctx, storage, clone.ID).Extract()
	require.NoError(t, err)
	assert.Equal(t, clone, pending)

	control(volumeCompleteCreatePath(clone.ID))

	completed, err := volumes.Get(ctx, storage, clone.ID).Extract()
	require.NoError(t, err)
	assert.Equal(t, "available", completed.Status)
	assert.Equal(t, ready.ID, completed.SourceVolID)
	assert.Equal(t, ready.Size, completed.Size)
	assert.Equal(t, "true", completed.Bootable)
	assert.Equal(t, ready.VolumeImageMetadata, completed.VolumeImageMetadata)
	assert.Empty(t, completed.Attachments)
	assert.Equal(t, fields, completed.Metadata)
	pages, err := volumes.List(storage, volumes.ListOpts{Name: name}).AllPages(ctx)
	require.NoError(t, err)
	items, err := volumes.ExtractVolumes(pages)
	require.NoError(t, err)
	assert.Equal(t, []volumes.Volume{*completed}, items)

	_, err = volumes.Get(ctx, storage, ready.ID).Extract()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusServiceUnavailable), "source detail outage remains pending: %v", err)
	after, err := volumes.Get(ctx, storage, ready.ID).Extract()
	require.NoError(t, err)
	assert.Equal(t, attached, after)

	retained, err := volumeattach.Get(ctx, compute, fixtureServerID, ready.ID).Extract()
	require.NoError(t, err)
	assert.Equal(t, attachment, retained)
}

func TestCinderVolumeCloneWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	path := volumePath + "/" + volumeProjectID(t, provider) + "/volumes"
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	admin := map[string]string{"X-SandStack-Admin-Token": adminToken}
	status, _, body := request(t, instance, http.MethodPost, path, `{"volume":{"size":1}}`, auth)
	require.Equal(t, http.StatusAccepted, status, "%s", body)
	var pending struct {
		Volume volumes.Volume `json:"volume"`
	}
	require.NoError(t, json.Unmarshal(body, &pending))
	require.NotEmpty(t, pending.Volume.ID)

	// https://docs.openstack.org/api-ref/block-storage/v3/#create-a-volume
	clone := func(sourceID string, want int) []byte {
		t.Helper()
		payload := fmt.Sprintf(`{"volume":{"source_volid":%q,"size":null}}`, sourceID)
		status, headers, body := request(t, instance, http.MethodPost, path, payload, auth)
		require.Equal(t, want, status, "%s", body)
		requireJSON(t, headers)
		return body
	}
	clone(pending.Volume.ID, http.StatusBadRequest)
	status, _, body = request(t, instance, http.MethodPut,
		"/_sandstack/volume/volumes/"+pending.Volume.ID+"/fault", "", admin)
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	clone(pending.Volume.ID, http.StatusBadRequest)
	clone("00000000-0000-0000-0000-000000000000", http.StatusNotFound)

	status, _, body = request(t, instance, http.MethodGet, path+"/detail", "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	var list struct {
		Volumes []wireVolume `json:"volumes"`
	}
	require.NoError(t, json.Unmarshal(body, &list))
	assert.Len(t, list.Volumes, 2, "invalid clone requests did not create volumes")

	body = clone(fixtureVolumeID, http.StatusAccepted)
	var result struct {
		Volume map[string]json.RawMessage `json:"volume"`
	}
	require.NoError(t, json.Unmarshal(body, &result))
	assert.JSONEq(t, fmt.Sprintf("%q", fixtureVolumeID), string(result.Volume["source_volid"]))
	assert.JSONEq(t, "1", string(result.Volume["size"]))
	assert.JSONEq(t, `"creating"`, string(result.Volume["status"]))
	assert.JSONEq(t, `[]`, string(result.Volume["attachments"]))
}
