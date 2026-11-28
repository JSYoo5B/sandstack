package server_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/snapshots"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/instanceactions"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/servers"
	"github.com/gophercloud/gophercloud/v2/openstack/image/v2/images"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newSnapshotImageClient(t *testing.T, provider *gophercloud.ProviderClient) *gophercloud.ServiceClient {
	t.Helper()
	client, err := openstack.NewImageV2(provider, gophercloud.EndpointOpts{
		Region: "RegionOne", Availability: gophercloud.AvailabilityPublic,
	})
	require.NoError(t, err)
	return client
}

func waitForImage(t *testing.T, client *gophercloud.ServiceClient, id string) *images.Image {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	var image *images.Image
	require.NoError(t, gophercloud.WaitFor(ctx, func(ctx context.Context) (bool, error) {
		current, err := images.Get(ctx, client, id).Extract()
		if err != nil {
			return false, err
		}
		image = current
		return current.Status == images.ImageStatusActive, nil
	}))
	return image
}

func bootImageServer(t *testing.T, client *gophercloud.ServiceClient, name, imageID string) string {
	t.Helper()
	created, err := servers.Create(t.Context(), client, servers.CreateOpts{
		Name: name, ImageRef: imageID, FlavorRef: "1",
	}, nil).Extract()
	require.NoError(t, err)
	waitForServer(t, client, created.ID, "ACTIVE")
	return created.ID
}

// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/servers.py#L1425-L1488
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L3454-L3511
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/utils.py#L1253-L1335
func TestGophercloudSnapshotsServerAndBootsFromImage(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	clients := newBootClients(t, instance)
	imageClient := newSnapshotImageClient(t, provider)
	ctx := t.Context()
	prepareBootNetwork(t, clients.network)
	serverID := bootImageServer(t, clients.compute, "web", fixtureImageID)
	fixture, err := images.Get(ctx, imageClient, fixtureImageID).Extract()
	require.NoError(t, err)

	imageID, err := servers.CreateImage(ctx, clients.compute, serverID, servers.CreateImageOpts{
		Name: "web-snap", Metadata: map[string]string{"purpose": "backup"},
	}).ExtractImageID()

	require.NoError(t, err)
	image := waitForImage(t, imageClient, imageID)
	assert.Equal(t, "web-snap", image.Name)
	assert.Equal(t, images.ImageVisibilityPrivate, image.Visibility)
	assert.Equal(t, fixture.Owner, image.Owner)
	assert.Equal(t, "bare", image.ContainerFormat)
	assert.Equal(t, "qcow2", image.DiskFormat)
	assert.Equal(t, 1, image.MinDiskGigabytes)
	assert.Equal(t, fixture.SizeBytes, image.SizeBytes)
	assert.Equal(t, fixture.Checksum, image.Checksum)
	for key, value := range map[string]string{
		"image_type":     "snapshot",
		"instance_uuid":  serverID,
		"base_image_ref": fixtureImageID,
		"purpose":        "backup",
		"image_location": "snapshot",
		"owner_id":       fixture.Owner,
	} {
		assert.Equal(t, value, image.Properties[key], key)
	}
	server, err := servers.Get(ctx, clients.compute, serverID).Extract()
	require.NoError(t, err)
	assert.Empty(t, server.TaskState)
	pages, err := instanceactions.List(clients.compute, serverID, nil).AllPages(ctx)
	require.NoError(t, err)
	actions, err := instanceactions.ExtractInstanceActions(pages)
	require.NoError(t, err)
	require.NotEmpty(t, actions)
	assert.Equal(t, "createImage", actions[0].Action)

	restoredID := bootImageServer(t, clients.compute, "restored", imageID)

	restored, err := servers.Get(ctx, clients.compute, restoredID).Extract()
	require.NoError(t, err)
	assert.Equal(t, imageID, restored.Image["id"])
	require.NoError(t, servers.Delete(ctx, clients.compute, restoredID).ExtractErr())
	require.NoError(t, servers.Delete(ctx, clients.compute, serverID).ExtractErr())
	require.NoError(t, images.Delete(ctx, imageClient, imageID).ExtractErr())
	_, err = images.Get(ctx, imageClient, imageID).Extract()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusNotFound), "%v", err)
}

// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L3513-L3653
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/block_device.py#L302-L313
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/image/glance.py#L523-L675
func TestGophercloudSnapshotsVolumeBackedServer(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	clients := newBootClients(t, instance)
	imageClient := newSnapshotImageClient(t, provider)
	ctx := t.Context()
	prepareBootNetwork(t, clients.network)
	rootID := createBootableVolume(t, instance, clients.volume)
	created, err := servers.Create(ctx, clients.compute, servers.CreateOpts{
		Name: "db", FlavorRef: "1",
		BlockDevice: []servers.BlockDevice{{
			SourceType: servers.SourceVolume, DestinationType: servers.DestinationVolume, UUID: rootID,
			BootIndex: 0, DeleteOnTermination: false,
		}},
	}, nil).Extract()
	require.NoError(t, err)
	waitForServer(t, clients.compute, created.ID, "ACTIVE")

	imageID, err := servers.CreateImage(ctx, clients.compute, created.ID, servers.CreateImageOpts{
		Name: "db-snap",
	}).ExtractImageID()

	require.NoError(t, err)
	image := waitForImage(t, imageClient, imageID)
	assert.Equal(t, int64(0), image.SizeBytes)
	assert.Equal(t, "raw", image.DiskFormat)
	assert.Equal(t, "bare", image.ContainerFormat)
	assert.Equal(t, "/dev/vda", image.Properties["root_device_name"])
	assert.Equal(t, "True", image.Properties["bdm_v2"])
	assert.NotContains(t, image.Properties, "image_type")
	encoded, ok := image.Properties["block_device_mapping"].(string)
	require.True(t, ok)
	var mappings []map[string]any
	require.NoError(t, json.Unmarshal([]byte(encoded), &mappings))
	require.Len(t, mappings, 1)
	assert.Equal(t, "snapshot", mappings[0]["source_type"])
	assert.Equal(t, "volume", mappings[0]["destination_type"])
	assert.Equal(t, float64(0), mappings[0]["boot_index"])
	assert.Equal(t, "/dev/vda", mappings[0]["device_name"])
	snapshotID, ok := mappings[0]["snapshot_id"].(string)
	require.True(t, ok)
	snapshot, err := snapshots.Get(ctx, clients.volume, snapshotID).Extract()
	require.NoError(t, err)
	assert.Equal(t, rootID, snapshot.VolumeID)
	assert.Equal(t, "snapshot for db-snap", snapshot.Name)

	_, bootErr := servers.Create(ctx, clients.compute, servers.CreateOpts{
		Name: "restored", ImageRef: imageID, FlavorRef: "1",
	}, nil).Extract()

	assert.True(t, gophercloud.ResponseCodeIs(bootErr, http.StatusBadRequest), "%v", bootErr)
}

func TestNovaCreateImageWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	clients := newBootClients(t, instance)
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	admin := map[string]string{"X-SandStack-Admin-Token": adminToken}
	prepareBootNetwork(t, clients.network)
	serverID := bootImageServer(t, clients.compute, "web", fixtureImageID)
	action := func(id, body string) (int, http.Header, []byte) {
		t.Helper()
		return request(t, instance, http.MethodPost, computePath+"/servers/"+id+"/action", body, auth)
	}
	control := func(method, path string) int {
		t.Helper()
		status, _, body := request(t, instance, method, "/_sandstack/compute"+path, "", admin)
		require.NotEqual(t, http.StatusUnauthorized, status, "%s", body)
		return status
	}
	imageStatus := func(location string) (int, string) {
		t.Helper()
		status, _, body := request(t, instance, http.MethodGet,
			strings.Replace(strings.TrimPrefix(location, instance.URL), "/image/images/", "/image/v2/images/", 1),
			"", auth)
		var image struct {
			Status string `json:"status"`
		}
		if status == http.StatusOK {
			require.NoError(t, json.Unmarshal(body, &image))
		}
		return status, image.Status
	}
	taskState := func() any {
		t.Helper()
		status, _, body := request(t, instance, http.MethodGet, computePath+"/servers/"+serverID, "", auth)
		require.Equal(t, http.StatusOK, status, "%s", body)
		var detail struct {
			Server map[string]any `json:"server"`
		}
		require.NoError(t, json.Unmarshal(body, &detail))
		return detail.Server["OS-EXT-STS:task_state"]
	}

	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/servers.py#L1475-L1488
	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/image/glance.py#L91-L116
	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/image/glance.py#L1101-L1107
	status, headers, body := action(serverID, `{"createImage":{"name":"web-snap","metadata":{"purpose":"backup"}}}`)

	require.Equal(t, http.StatusAccepted, status, "%s", body)
	assert.Empty(t, body)
	location := headers.Get("Location")
	assert.Regexp(t, "^"+instance.URL+"/image/images/[0-9a-f-]{36}$", location)
	imageReadStatus, state := imageStatus(location)
	assert.Equal(t, http.StatusOK, imageReadStatus)
	assert.Equal(t, "active", state)
	requestID := headers.Get("X-Openstack-Request-Id")
	status, _, body = request(t, instance, http.MethodGet,
		computePath+"/servers/"+serverID+"/os-instance-actions/"+requestID, "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	assert.Contains(t, string(body), `"action":"createImage"`)

	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/schemas/servers.py#L484-L499
	for _, body := range []string{
		`{"createImage":{}}`,
		`{"createImage":{"name":""}}`,
		`{"createImage":{"name":" padded"}}`,
		`{"createImage":{"name":"snap","extra":true}}`,
		`{"createImage":{"name":"snap","metadata":{"key":1}}}`,
		`{"createImage":{"name":"snap","metadata":{"bad/key":"value"}}}`,
		`{"createImage":"snap"}`,
	} {
		status, headers, response := action(serverID, body)
		requireNovaFault(t, http.StatusBadRequest, "badRequest", status, headers, response)
	}

	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/common.py#L282-L289
	items := make([]string, 129)
	for i := range items {
		items[i] = fmt.Sprintf(`"key-%d":"value"`, i)
	}
	status, headers, body = action(serverID, `{"createImage":{"name":"snap","metadata":{`+
		strings.Join(items, ",")+`}}}`)
	requireNovaFault(t, http.StatusForbidden, "forbidden", status, headers, body)

	status, headers, body = action("00000000-0000-4000-8000-000000000000", `{"createImage":{"name":"snap"}}`)
	requireNovaFault(t, http.StatusNotFound, "itemNotFound", status, headers, body)

	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L142-L180
	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/common.py#L330-L346
	require.Equal(t, http.StatusNoContent, control(http.MethodPut, "/hold-builds"))
	building, err := servers.Create(t.Context(), clients.compute, servers.CreateOpts{
		Name: "building", ImageRef: fixtureImageID, FlavorRef: "1",
	}, nil).Extract()
	require.NoError(t, err)
	status, headers, body = action(building.ID, `{"createImage":{"name":"snap"}}`)
	requireNovaFault(t, http.StatusConflict, "conflictingRequest", status, headers, body)
	assert.Contains(t, string(body), "Cannot 'createImage' instance "+building.ID+" while it is in vm_state building")

	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/manager.py#L219-L237
	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/manager.py#L4583-L4697
	require.Equal(t, http.StatusNoContent, control(http.MethodPut, "/hold-images"))
	assert.Nil(t, taskState())

	status, headers, body = action(serverID, `{"createImage":{"name":"held"}}`)

	require.Equal(t, http.StatusAccepted, status, "%s", body)
	held := headers.Get("Location")
	_, state = imageStatus(held)
	assert.Equal(t, "queued", state)
	assert.Equal(t, "image_snapshot_pending", taskState())
	status, headers, body = action(serverID, `{"createImage":{"name":"again"}}`)
	requireNovaFault(t, http.StatusConflict, "conflictingRequest", status, headers, body)
	assert.Contains(t, string(body), "while it is in task_state image_snapshot_pending")
	status, headers, body = action(serverID, `{"pause":null}`)
	requireNovaFault(t, http.StatusConflict, "conflictingRequest", status, headers, body)

	require.Equal(t, http.StatusNoContent, control(http.MethodPut, "/servers/"+serverID+"/complete-image"))

	_, state = imageStatus(held)
	assert.Equal(t, "active", state)
	assert.Nil(t, taskState())
	assert.Equal(t, http.StatusConflict, control(http.MethodPut, "/servers/"+serverID+"/complete-image"))
	assert.Equal(t, http.StatusNotFound, control(http.MethodPut, "/servers/missing/fail-image"))

	status, headers, body = action(serverID, `{"createImage":{"name":"failed"}}`)
	require.Equal(t, http.StatusAccepted, status, "%s", body)
	failed := headers.Get("Location")

	require.Equal(t, http.StatusNoContent, control(http.MethodPut, "/servers/"+serverID+"/fail-image"))

	failedStatus, _ := imageStatus(failed)
	assert.Equal(t, http.StatusNotFound, failedStatus)
	assert.Nil(t, taskState())
	require.Equal(t, http.StatusNoContent, control(http.MethodDelete, "/hold-images"))
	status, headers, body = action(serverID, `{"createImage":{"name":"immediate"}}`)
	require.Equal(t, http.StatusAccepted, status, "%s", body)
	_, state = imageStatus(headers.Get("Location"))
	assert.Equal(t, "active", state)
}
