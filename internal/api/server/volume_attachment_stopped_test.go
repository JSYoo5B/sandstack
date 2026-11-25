package server_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/volumes"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/servers"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/volumeattach"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGophercloudAttachesAndDetachesVolumeWhileServerIsStopped(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	endpoint := gophercloud.EndpointOpts{Region: "RegionOne", Availability: gophercloud.AvailabilityPublic}
	compute, err := openstack.NewComputeV2(provider, endpoint)
	require.NoError(t, err)
	storage, err := openstack.NewBlockStorageV3(provider, endpoint)
	require.NoError(t, err)
	ctx := t.Context()
	volume, err := volumes.Create(ctx, storage, volumes.CreateOpts{Name: "stopped-server-volume", Size: 1}, nil).Extract()
	require.NoError(t, err)
	require.NotEmpty(t, volume.ID)
	status, _, body := request(t, instance, http.MethodPut, volumeCompleteCreatePath(volume.ID), "",
		map[string]string{"X-SandStack-Admin-Token": adminToken})
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	readVolume := func() *volumes.Volume {
		t.Helper()
		result, err := volumes.Get(ctx, storage, volume.ID).Extract()
		require.NoError(t, err)
		return result
	}
	assert.Equal(t, "available", readVolume().Status)
	require.NoError(t, servers.Stop(ctx, compute, fixtureServerID).ExtractErr())

	checkStopped := func(want []servers.AttachedVolume) {
		t.Helper()
		server, err := servers.Get(ctx, compute, fixtureServerID).Extract()
		require.NoError(t, err)
		assert.Equal(t, "SHUTOFF", server.Status)
		assert.Equal(t, "stopped", server.VmState)
		assert.Equal(t, servers.PowerState(servers.SHUTDOWN), server.PowerState)
		assert.Equal(t, want, server.AttachedVolumes)
	}
	checkStopped([]servers.AttachedVolume{})
	const device = "/dev/vdc"
	attached, err := volumeattach.Create(ctx, compute, fixtureServerID, volumeattach.CreateOpts{
		VolumeID: volume.ID, Device: device,
	}).Extract()

	require.NoError(t, err)
	wantAttachment := &volumeattach.VolumeAttachment{
		ID: volume.ID, VolumeID: volume.ID, ServerID: fixtureServerID, Device: device,
	}
	assert.Equal(t, wantAttachment, attached)
	record, err := volumeattach.Get(ctx, compute, fixtureServerID, volume.ID).Extract()
	require.NoError(t, err)
	assert.Equal(t, wantAttachment, record)
	checkStopped([]servers.AttachedVolume{{ID: volume.ID}})
	inUse := readVolume()
	assert.Equal(t, "in-use", inUse.Status)
	require.Len(t, inUse.Attachments, 1)
	assert.Equal(t, fixtureServerID, inUse.Attachments[0].ServerID)
	assert.Equal(t, volume.ID, inUse.Attachments[0].VolumeID)
	assert.Equal(t, device, inUse.Attachments[0].Device)

	require.NoError(t, volumeattach.Delete(ctx, compute, fixtureServerID, volume.ID).ExtractErr())

	_, err = volumeattach.Get(ctx, compute, fixtureServerID, volume.ID).Extract()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusNotFound), "removed attachment: %v", err)
	checkStopped([]servers.AttachedVolume{})
	available := readVolume()
	assert.Equal(t, "available", available.Status)
	assert.Empty(t, available.Attachments)

	// https://docs.openstack.org/api-ref/compute/#attach-a-volume-to-an-instance
	collection := computePath + "/" + volumeProjectID(t, provider) + fixtureAttachmentPath
	auth := map[string]string{"X-Auth-Token": provider.Token(), "X-OpenStack-Nova-API-Version": "2.1"}
	payload := fmt.Sprintf(`{"volumeAttachment":{"volumeId":%q,"device":%q}}`, volume.ID, device)
	status, headers, body := request(t, instance, http.MethodPost, collection, payload, auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	var response struct {
		Attachment wireNovaAttachment `json:"volumeAttachment"`
	}
	require.NoError(t, json.Unmarshal(body, &response))
	assert.Equal(t, wireNovaAttachment{
		ID: volume.ID, VolumeID: volume.ID, ServerID: fixtureServerID, Device: device,
	}, response.Attachment)
	checkStopped([]servers.AttachedVolume{{ID: volume.ID}})

	// https://docs.openstack.org/api-ref/compute/#detach-a-volume-from-an-instance
	status, _, body = request(t, instance, http.MethodDelete, collection+"/"+volume.ID, "", auth)
	require.Equal(t, http.StatusAccepted, status, "%s", body)
	assert.Empty(t, body)
	checkStopped([]servers.AttachedVolume{})
	assert.Equal(t, "available", readVolume().Status)
}
