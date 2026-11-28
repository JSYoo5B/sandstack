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

func TestGophercloudSuspendsAndResumesNovaServer(t *testing.T) {
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
		assert.Equal(t, []servers.AttachedVolume{{ID: fixtureVolumeID}}, server.AttachedVolumes)
	}
	conflict := func(err error, action string) {
		t.Helper()
		assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusConflict), "%s: %v", action, err)
	}
	_, err = volumeattach.Create(ctx, compute, fixtureServerID, volumeattach.CreateOpts{
		VolumeID: fixtureVolumeID,
	}).Extract()
	require.NoError(t, err)
	conflict(servers.Resume(ctx, compute, fixtureServerID).ExtractErr(), "resume active server")

	require.NoError(t, servers.Suspend(ctx, compute, fixtureServerID).ExtractErr())

	suspended := listComputeServers(t, compute, "SUSPENDED")
	require.Len(t, suspended, 1)
	check(suspended[0], "SUSPENDED", "suspended", servers.SHUTDOWN)
	assert.Empty(t, listComputeServers(t, compute, "ACTIVE"))
	detail, err := servers.Get(ctx, compute, fixtureServerID).Extract()
	require.NoError(t, err)
	check(*detail, "SUSPENDED", "suspended", servers.SHUTDOWN)
	conflict(servers.Suspend(ctx, compute, fixtureServerID).ExtractErr(), "repeat suspend")
	conflict(servers.Pause(ctx, compute, fixtureServerID).ExtractErr(), "pause suspended server")
	conflict(servers.Stop(ctx, compute, fixtureServerID).ExtractErr(), "stop suspended server")
	conflict(servers.Start(ctx, compute, fixtureServerID).ExtractErr(), "start suspended server")
	soft := servers.RebootOpts{Type: servers.SoftReboot}
	conflict(servers.Reboot(ctx, compute, fixtureServerID, soft).ExtractErr(), "soft reboot suspended server")
	err = volumeattach.Delete(ctx, compute, fixtureServerID, fixtureVolumeID).ExtractErr()
	conflict(err, "detach from suspended server")

	require.NoError(t, servers.Resume(ctx, compute, fixtureServerID).ExtractErr())

	active := listComputeServers(t, compute, "ACTIVE")
	require.Len(t, active, 1)
	check(active[0], "ACTIVE", "active", servers.RUNNING)
	assert.Empty(t, listComputeServers(t, compute, "SUSPENDED"))
	conflict(servers.Resume(ctx, compute, fixtureServerID).ExtractErr(), "repeat resume")

	require.NoError(t, servers.Suspend(ctx, compute, fixtureServerID).ExtractErr())
	hard := servers.RebootOpts{Type: servers.HardReboot}
	require.NoError(t, servers.Reboot(ctx, compute, fixtureServerID, hard).ExtractErr())

	detail, err = servers.Get(ctx, compute, fixtureServerID).Extract()
	require.NoError(t, err)
	check(*detail, "ACTIVE", "active", servers.RUNNING)
}

func TestNovaSuspendActionsWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	projectPath := computePath + "/" + routerProjectID(t, provider)
	path := projectPath + "/servers/" + fixtureServerID
	action := path + "/action"
	attachments := projectPath + fixtureAttachmentPath
	auth := map[string]string{"X-Auth-Token": provider.Token(), "OpenStack-API-Version": "compute 2.1"}
	read := func() wireComputeServer {
		t.Helper()
		status, headers, body := request(t, instance, http.MethodGet, path, "", auth)
		require.Equal(t, http.StatusOK, status, "%s", body)
		requireJSON(t, headers)
		return decodeWireComputeServer(t, body)
	}

	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/schemas/suspend_server.py#L13-L31
	for _, body := range []string{
		`{"suspend":null,"resume":null}`,
		`{"suspend":null,"other":true}`,
		`{"suspend":null,"suspend":null}`,
	} {
		status, headers, response := request(t, instance, http.MethodPost, action, body, auth)
		requireNovaFault(t, http.StatusBadRequest, "badRequest", status, headers, response)
	}
	const missing = "00000000-0000-0000-0000-000000000000"
	status, headers, body := request(t, instance, http.MethodPost,
		computePath+"/servers/"+missing+"/action", `{"suspend":null}`, auth)
	requireNovaFault(t, http.StatusNotFound, "itemNotFound", status, headers, body)
	status, headers, body = request(t, instance, http.MethodPost, action, `{"resume":null}`, auth)
	requireNovaFault(t, http.StatusConflict, "conflictingRequest", status, headers, body)

	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L4880-L4908
	status, headers, body = request(t, instance, http.MethodPost, action, `{"suspend":{}}`, auth)

	require.Equal(t, http.StatusAccepted, status, "%s", body)
	assert.Empty(t, body)
	assert.Equal(t, "compute 2.1", headers.Get("OpenStack-API-Version"))
	suspended := read()
	assert.Equal(t, "SUSPENDED", suspended.Status)
	assert.Equal(t, "suspended", suspended.VMState)
	require.NotNil(t, suspended.PowerState)
	assert.Equal(t, 4, *suspended.PowerState)
	assert.JSONEq(t, "null", string(suspended.TaskState))

	status, headers, body = request(t, instance, http.MethodPost, action, `{"suspend":null}`, auth)

	requireNovaFault(t, http.StatusConflict, "conflictingRequest", status, headers, body)
	status, headers, body = request(t, instance, http.MethodPost, action, `{"pause":null}`, auth)
	requireNovaFault(t, http.StatusConflict, "conflictingRequest", status, headers, body)

	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L5225-L5230
	status, headers, body = request(t, instance, http.MethodPost, attachments,
		`{"volumeAttachment":{"volumeId":"`+fixtureVolumeID+`"}}`, auth)

	requireNovaFault(t, http.StatusConflict, "conflictingRequest", status, headers, body)
	status, _, body = request(t, instance, http.MethodGet, attachments, "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	assert.JSONEq(t, `{"volumeAttachments":[]}`, string(body))

	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/volume_attachments.py#L411-L440
	status, headers, body = request(t, instance, http.MethodDelete, attachments+"/"+fixtureVolumeID, "", auth)

	requireNovaFault(t, http.StatusNotFound, "itemNotFound", status, headers, body)

	projectless := computePath + "/servers/" + fixtureServerID + "/action"
	status, headers, body = request(t, instance, http.MethodPost, projectless, `{"resume":null}`, auth)

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
