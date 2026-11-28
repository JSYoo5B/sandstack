package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/volumes"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/servers"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/volumeattach"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/subnets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createBootableVolume(t *testing.T, instance *httptest.Server, client *gophercloud.ServiceClient) string {
	t.Helper()
	volume, err := volumes.Create(t.Context(), client, volumes.CreateOpts{
		Name: "root", Size: 1, ImageID: fixtureImageID,
	}, nil).Extract()
	require.NoError(t, err)
	status, _, body := request(t, instance, http.MethodPut,
		"/_sandstack/volume/volumes/"+volume.ID+"/complete-create", "",
		map[string]string{"X-SandStack-Admin-Token": adminToken})
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	return volume.ID
}

func prepareBootNetwork(t *testing.T, client *gophercloud.ServiceClient) {
	t.Helper()
	_, err := subnets.Create(t.Context(), client, subnets.CreateOpts{
		NetworkID: fixtureNetworkID, CIDR: "10.20.0.0/24", IPVersion: gophercloud.IPv4,
	}).Extract()
	require.NoError(t, err)
}

// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/servers.py#L515-L563
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/volume_attachments.py#L426-L428
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L2695-L2724
func TestGophercloudBootsServerFromExistingVolume(t *testing.T) {
	instance := startServer(t, "")
	clients := newBootClients(t, instance)
	ctx := t.Context()
	prepareBootNetwork(t, clients.network)
	rootID := createBootableVolume(t, instance, clients.volume)

	created, err := servers.Create(ctx, clients.compute, servers.CreateOpts{
		Name: "web", FlavorRef: "1",
		BlockDevice: []servers.BlockDevice{{
			SourceType: servers.SourceVolume, DestinationType: servers.DestinationVolume, UUID: rootID,
			BootIndex: 0, DeleteOnTermination: false,
		}},
	}, nil).Extract()

	require.NoError(t, err)
	waitForServer(t, clients.compute, created.ID, "ACTIVE")
	server, err := servers.Get(ctx, clients.compute, created.ID).Extract()
	require.NoError(t, err)
	assert.Nil(t, server.Image)
	assert.Equal(t, []servers.AttachedVolume{{ID: rootID}}, server.AttachedVolumes)
	pages, err := volumeattach.List(clients.compute, created.ID).AllPages(ctx)
	require.NoError(t, err)
	attachments, err := volumeattach.ExtractVolumeAttachments(pages)
	require.NoError(t, err)
	require.Len(t, attachments, 1)
	assert.Equal(t, rootID, attachments[0].VolumeID)
	assert.Equal(t, "/dev/vda", attachments[0].Device)
	root, err := volumes.Get(ctx, clients.volume, rootID).Extract()
	require.NoError(t, err)
	assert.Equal(t, "in-use", root.Status)
	require.Len(t, root.Attachments, 1)
	assert.Equal(t, created.ID, root.Attachments[0].ServerID)
	assert.Equal(t, "/dev/vda", root.Attachments[0].Device)

	detachErr := volumeattach.Delete(ctx, clients.compute, created.ID, rootID).ExtractErr()
	deleteErr := servers.Delete(ctx, clients.compute, created.ID).ExtractErr()

	assert.True(t, gophercloud.ResponseCodeIs(detachErr, http.StatusBadRequest), "%v", detachErr)
	require.NoError(t, deleteErr)
	_, err = servers.Get(ctx, clients.compute, created.ID).Extract()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusNotFound), "%v", err)
	released, err := volumes.Get(ctx, clients.volume, rootID).Extract()
	require.NoError(t, err)
	assert.Equal(t, "available", released.Status)
	assert.Empty(t, released.Attachments)
}

// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/virt/block_device.py#L445-L470
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L2695-L2724
func TestGophercloudBootsServerFromImageVolume(t *testing.T) {
	instance := startServer(t, "")
	clients := newBootClients(t, instance)
	ctx := t.Context()
	prepareBootNetwork(t, clients.network)

	created, err := servers.Create(ctx, clients.compute, servers.CreateOpts{
		Name: "web", FlavorRef: "1",
		BlockDevice: []servers.BlockDevice{{
			SourceType: servers.SourceImage, DestinationType: servers.DestinationVolume, UUID: fixtureImageID,
			VolumeSize: 2, BootIndex: 0, DeleteOnTermination: true,
		}},
	}, nil).Extract()

	require.NoError(t, err)
	waitForServer(t, clients.compute, created.ID, "ACTIVE")
	server, err := servers.Get(ctx, clients.compute, created.ID).Extract()
	require.NoError(t, err)
	assert.Nil(t, server.Image)
	require.Len(t, server.AttachedVolumes, 1)
	rootID := server.AttachedVolumes[0].ID
	root, err := volumes.Get(ctx, clients.volume, rootID).Extract()
	require.NoError(t, err)
	assert.Equal(t, "in-use", root.Status)
	assert.Equal(t, 2, root.Size)
	assert.Empty(t, root.Name)
	assert.Equal(t, "true", root.Bootable)
	assert.Equal(t, fixtureImageID, root.VolumeImageMetadata["image_id"])
	require.Len(t, root.Attachments, 1)
	assert.Equal(t, "/dev/vda", root.Attachments[0].Device)

	require.NoError(t, servers.Delete(ctx, clients.compute, created.ID).ExtractErr())

	_, err = volumes.Get(ctx, clients.volume, rootID).Extract()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusNotFound), "%v", err)
}

func TestNovaBootFromVolumeWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	clients := newBootClients(t, instance)
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	prepareBootNetwork(t, clients.network)
	rootID := createBootableVolume(t, instance, clients.volume)
	const missing = "00000000-0000-4000-8000-000000000000"
	create := func(devices string) (int, http.Header, []byte) {
		t.Helper()
		return request(t, instance, http.MethodPost, computePath+"/servers",
			`{"server":{"name":"web","flavorRef":"1","block_device_mapping_v2":[`+devices+`]}}`, auth)
	}
	volumeDevice := func(id, fields string) string {
		return `{"source_type":"volume","destination_type":"volume","uuid":"` + id + `"` + fields + `}`
	}

	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/schemas/servers.py#L21-L85
	for _, devices := range []string{
		volumeDevice(rootID, `,"boot_index":"first"`),
		volumeDevice(rootID, `,"boot_index":0,"delete_on_termination":"maybe"`),
		volumeDevice(rootID, `,"boot_index":0,"volume_size":0`),
		volumeDevice(rootID, `,"boot_index":0,"tag":"added in 2.32"`),
		`{"source_type":"disk","uuid":"` + rootID + `","boot_index":0}`,
		volumeDevice(rootID, `,"boot_index":0,"device_name":"vda"`),
		volumeDevice(rootID, `,"boot_index":0`) + "," + volumeDevice(fixtureVolumeID, `,"boot_index":1`),
		`{"source_type":"snapshot","destination_type":"volume","uuid":"` + rootID + `","boot_index":0}`,
	} {
		status, headers, body := create(devices)
		requireNovaFault(t, http.StatusBadRequest, "badRequest", status, headers, body)
	}
	status, headers, body := request(t, instance, http.MethodPost, computePath+"/servers",
		`{"server":{"name":"web","imageRef":"`+fixtureImageID+`","flavorRef":"1","block_device_mapping_v2":[`+
			volumeDevice(rootID, `,"boot_index":0`)+`]}}`, auth)
	requireNovaFault(t, http.StatusBadRequest, "badRequest", status, headers, body)

	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/block_device.py#L174-L225
	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/block_device.py#L615-L668
	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L1985-L2074
	for _, test := range []struct {
		devices string
		message string
	}{
		{`{"source_type":"volume","destination_type":"volume","boot_index":0}`,
			"Block Device Mapping is Invalid: Missing device UUID."},
		{`{"source_type":"blank","destination_type":"volume","uuid":"` + rootID + `","boot_index":0}`,
			"Block Device Mapping is Invalid: Invalid device UUID."},
		{`{"source_type":"image","destination_type":"local","uuid":"` + fixtureImageID + `","boot_index":0}`,
			"Block Device Mapping is Invalid: Mapping image to local is not supported."},
		{volumeDevice(missing, `,"boot_index":0`),
			"Block Device Mapping is Invalid: failed to get volume " + missing + "."},
		{volumeDevice(fixtureVolumeID, `,"boot_index":0`), "Block Device " + fixtureVolumeID + " is not bootable."},
		{`{"source_type":"image","destination_type":"volume","uuid":"` + missing + `","boot_index":0,"volume_size":1}`,
			"Block Device Mapping is Invalid: failed to get image " + missing + "."},
		{volumeDevice(rootID, `,"boot_index":"-1"`), "Block Device Mapping is Invalid: Boot sequence for the " +
			"instance and image/block device mapping combination is not valid."},
		{`{"source_type":"image","destination_type":"volume","uuid":"` + fixtureImageID + `","boot_index":0}`,
			"Images with destination_type 'volume' need to have a non-zero size specified"},
	} {
		status, headers, body := create(test.devices)
		requireNovaFault(t, http.StatusBadRequest, "badRequest", status, headers, body)
		assert.Contains(t, string(body), test.message)
	}

	status, _, body = create(volumeDevice(rootID, `,"boot_index":"0","delete_on_termination":"yes"`))

	require.Equal(t, http.StatusAccepted, status, "%s", body)
	var created struct {
		Server struct {
			ID string `json:"id"`
		} `json:"server"`
	}
	require.NoError(t, json.Unmarshal(body, &created))
	status, _, body = request(t, instance, http.MethodGet, computePath+"/servers/"+created.Server.ID, "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	var detail wireCreatedServer
	require.NoError(t, json.Unmarshal(body, &detail))
	assert.JSONEq(t, `""`, string(detail.Server["image"]))
	assert.JSONEq(t, `[{"id":"`+rootID+`"}]`, string(detail.Server["os-extended-volumes:volumes_attached"]))

	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L2056-L2073
	status, headers, body = create(volumeDevice(rootID, `,"boot_index":0`))

	requireNovaFault(t, http.StatusBadRequest, "badRequest", status, headers, body)
	assert.Contains(t, string(body), "Invalid volume: Invalid input received: Invalid volume: Volume "+rootID+
		" status must be available or downloading to reserve, but the current status is in-use.")
	status, headers, body = request(t, instance, http.MethodDelete,
		computePath+"/servers/"+created.Server.ID+"/os-volume_attachments/"+rootID, "", auth)
	requireNovaFault(t, http.StatusBadRequest, "badRequest", status, headers, body)
	assert.Contains(t, string(body), "Cannot detach a root device volume")
	status, _, body = request(t, instance, http.MethodDelete, computePath+"/servers/"+created.Server.ID, "", auth)
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	status, _, body = request(t, instance, http.MethodGet, volumePath+"/volumes/"+rootID, "", auth)
	assert.Equal(t, http.StatusNotFound, status, "%s", body)
}

// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/conductor/manager.py#L1650-L1655
func TestNovaHeldVolumeBuildWire(t *testing.T) {
	instance := startServer(t, "")
	clients := newBootClients(t, instance)
	admin := map[string]string{"X-SandStack-Admin-Token": adminToken}
	prepareBootNetwork(t, clients.network)
	rootID := createBootableVolume(t, instance, clients.volume)
	status, _, body := request(t, instance, http.MethodPut, "/_sandstack/compute/hold-builds", "", admin)
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	created, err := servers.Create(t.Context(), clients.compute, servers.CreateOpts{
		Name: "web", FlavorRef: "1",
		BlockDevice: []servers.BlockDevice{{
			SourceType: servers.SourceVolume, DestinationType: servers.DestinationVolume, UUID: rootID,
		}},
	}, nil).Extract()
	require.NoError(t, err)

	reserved, err := volumes.Get(t.Context(), clients.volume, rootID).Extract()

	require.NoError(t, err)
	assert.Equal(t, "reserved", reserved.Status)
	assert.Empty(t, reserved.Attachments)

	status, _, body = request(t, instance, http.MethodPut,
		"/_sandstack/compute/servers/"+created.ID+"/complete-create", "", admin)

	require.Equal(t, http.StatusNoContent, status, "%s", body)
	attached, err := volumes.Get(t.Context(), clients.volume, rootID).Extract()
	require.NoError(t, err)
	assert.Equal(t, "in-use", attached.Status)
	require.Len(t, attached.Attachments, 1)
	assert.Equal(t, created.ID, attached.Attachments[0].ServerID)
}
