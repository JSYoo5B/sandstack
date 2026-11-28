package server_test

import (
	"net/http"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/servers"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/volumeattach"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGophercloudPausesAndUnpausesNovaServer(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	compute, err := openstack.NewComputeV2(provider, gophercloud.EndpointOpts{
		Region: "RegionOne", Availability: gophercloud.AvailabilityPublic,
	})
	require.NoError(t, err)
	ctx := t.Context()
	check := func(server servers.Server, status, vmState string, power servers.PowerState) {
		t.Helper()
		assert.Equal(t, fixtureServerID, server.ID)
		assert.Equal(t, status, server.Status)
		assert.Equal(t, vmState, server.VmState)
		assert.Equal(t, power, server.PowerState)
	}
	conflict := func(err error, action string) {
		t.Helper()
		assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusConflict), "%s: %v", action, err)
	}
	err = servers.Unpause(ctx, compute, fixtureServerID).ExtractErr()
	conflict(err, "unpause active server")

	require.NoError(t, servers.Pause(ctx, compute, fixtureServerID).ExtractErr())

	paused := listComputeServers(t, compute, "PAUSED")
	require.Len(t, paused, 1)
	check(paused[0], "PAUSED", "paused", servers.PAUSED)
	assert.Empty(t, listComputeServers(t, compute, "ACTIVE"))
	detail, err := servers.Get(ctx, compute, fixtureServerID).Extract()
	require.NoError(t, err)
	check(*detail, "PAUSED", "paused", servers.PAUSED)
	conflict(servers.Pause(ctx, compute, fixtureServerID).ExtractErr(), "repeat pause")
	conflict(servers.Stop(ctx, compute, fixtureServerID).ExtractErr(), "stop paused server")
	conflict(servers.Start(ctx, compute, fixtureServerID).ExtractErr(), "start paused server")
	soft := servers.RebootOpts{Type: servers.SoftReboot}
	conflict(servers.Reboot(ctx, compute, fixtureServerID, soft).ExtractErr(), "soft reboot paused server")

	attachment, err := volumeattach.Create(ctx, compute, fixtureServerID, volumeattach.CreateOpts{
		VolumeID: fixtureVolumeID,
	}).Extract()

	require.NoError(t, err)
	assert.Equal(t, fixtureVolumeID, attachment.VolumeID)

	require.NoError(t, servers.Unpause(ctx, compute, fixtureServerID).ExtractErr())

	active := listComputeServers(t, compute, "ACTIVE")
	require.Len(t, active, 1)
	check(active[0], "ACTIVE", "active", servers.RUNNING)
	assert.Equal(t, []servers.AttachedVolume{{ID: fixtureVolumeID}}, active[0].AttachedVolumes)
	assert.Empty(t, listComputeServers(t, compute, "PAUSED"))
	conflict(servers.Unpause(ctx, compute, fixtureServerID).ExtractErr(), "repeat unpause")

	require.NoError(t, servers.Pause(ctx, compute, fixtureServerID).ExtractErr())
	hard := servers.RebootOpts{Type: servers.HardReboot}
	require.NoError(t, servers.Reboot(ctx, compute, fixtureServerID, hard).ExtractErr())

	detail, err = servers.Get(ctx, compute, fixtureServerID).Extract()
	require.NoError(t, err)
	check(*detail, "ACTIVE", "active", servers.RUNNING)
}

func TestNovaPauseActionsWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	path := computePath + "/" + routerProjectID(t, provider) + "/servers/" + fixtureServerID
	action := path + "/action"
	auth := map[string]string{"X-Auth-Token": provider.Token(), "OpenStack-API-Version": "compute 2.1"}
	read := func() wireComputeServer {
		t.Helper()
		status, headers, body := request(t, instance, http.MethodGet, path, "", auth)
		require.Equal(t, http.StatusOK, status, "%s", body)
		requireJSON(t, headers)
		return decodeWireComputeServer(t, body)
	}

	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/schemas/pause_server.py#L13-L31
	for _, body := range []string{
		`{"pause":null,"unpause":null}`,
		`{"pause":null,"other":true}`,
		`{"pause":null,"pause":null}`,
	} {
		status, headers, response := request(t, instance, http.MethodPost, action, body, auth)
		requireNovaFault(t, http.StatusBadRequest, "badRequest", status, headers, response)
	}
	const missing = "00000000-0000-0000-0000-000000000000"
	status, headers, body := request(t, instance, http.MethodPost,
		computePath+"/servers/"+missing+"/action", `{"pause":null}`, auth)
	requireNovaFault(t, http.StatusNotFound, "itemNotFound", status, headers, body)
	status, headers, body = request(t, instance, http.MethodPost, action, `{"unpause":null}`, auth)
	requireNovaFault(t, http.StatusConflict, "conflictingRequest", status, headers, body)

	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L4851-L4867
	status, headers, body = request(t, instance, http.MethodPost, action, `{"pause":{}}`, auth)

	require.Equal(t, http.StatusAccepted, status, "%s", body)
	assert.Empty(t, body)
	assert.Equal(t, "compute 2.1", headers.Get("OpenStack-API-Version"))
	paused := read()
	assert.Equal(t, "PAUSED", paused.Status)
	assert.Equal(t, "paused", paused.VMState)
	require.NotNil(t, paused.PowerState)
	assert.Equal(t, 3, *paused.PowerState)
	assert.JSONEq(t, "null", string(paused.TaskState))

	status, headers, body = request(t, instance, http.MethodPost, action, `{"pause":null}`, auth)

	requireNovaFault(t, http.StatusConflict, "conflictingRequest", status, headers, body)
	status, headers, body = request(t, instance, http.MethodPost, action, `{"os-stop":null}`, auth)
	requireNovaFault(t, http.StatusConflict, "conflictingRequest", status, headers, body)

	projectless := computePath + "/servers/" + fixtureServerID + "/action"
	status, headers, body = request(t, instance, http.MethodPost, projectless, `{"unpause":null}`, auth)

	require.Equal(t, http.StatusAccepted, status, "%s", body)
	assert.Empty(t, body)
	assert.Equal(t, "2.1", headers.Get("X-OpenStack-Nova-API-Version"))
	active := read()
	assert.Equal(t, "ACTIVE", active.Status)
	assert.Equal(t, "active", active.VMState)
	require.NotNil(t, active.PowerState)
	assert.Equal(t, 1, *active.PowerState)
	assert.JSONEq(t, "null", string(active.TaskState))
}
