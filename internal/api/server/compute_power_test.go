package server_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/servers"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/volumeattach"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGophercloudStopsAndStartsNovaServer(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	compute, err := openstack.NewComputeV2(provider, gophercloud.EndpointOpts{
		Region: "RegionOne", Availability: gophercloud.AvailabilityPublic,
	})
	require.NoError(t, err)
	ctx := t.Context()
	attachment, err := volumeattach.Create(ctx, compute, fixtureServerID, volumeattach.CreateOpts{
		VolumeID: fixtureVolumeID,
	}).Extract()
	require.NoError(t, err)
	require.Equal(t, fixtureVolumeID, attachment.VolumeID)
	check := func(server servers.Server, status, vmState string, power servers.PowerState) {
		t.Helper()
		assert.Equal(t, fixtureServerID, server.ID)
		assert.Equal(t, status, server.Status)
		assert.Equal(t, vmState, server.VmState)
		assert.Equal(t, power, server.PowerState)
		require.Len(t, server.AttachedVolumes, 1)
		assert.Equal(t, fixtureVolumeID, server.AttachedVolumes[0].ID)
	}
	detail, err := servers.Get(ctx, compute, fixtureServerID).Extract()
	require.NoError(t, err)
	check(*detail, "ACTIVE", "active", servers.RUNNING)
	err = servers.Start(ctx, compute, fixtureServerID).ExtractErr()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusConflict), "start running server: %v", err)

	status, _, body := request(t, instance, http.MethodPut, unavailableControl, "", map[string]string{
		"X-SandStack-Admin-Token": adminToken,
	})
	require.Equal(t, http.StatusNoContent, status, "%s", body)

	require.NoError(t, servers.Stop(ctx, compute, fixtureServerID).ExtractErr())

	stopped := listComputeServers(t, compute, "SHUTOFF")
	require.Len(t, stopped, 1)
	check(stopped[0], "SHUTOFF", "stopped", servers.SHUTDOWN)
	assert.Empty(t, listComputeServers(t, compute, "ACTIVE"))
	err = servers.Stop(ctx, compute, fixtureServerID).ExtractErr()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusConflict), "stop stopped server: %v", err)
	_, err = servers.Get(ctx, compute, fixtureServerID).Extract()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusServiceUnavailable), "one-shot read remains armed: %v", err)
	detail, err = servers.Get(ctx, compute, fixtureServerID).Extract()
	require.NoError(t, err)
	check(*detail, "SHUTOFF", "stopped", servers.SHUTDOWN)

	require.NoError(t, servers.Start(ctx, compute, fixtureServerID).ExtractErr())

	active := listComputeServers(t, compute, "ACTIVE")
	require.Len(t, active, 1)
	check(active[0], "ACTIVE", "active", servers.RUNNING)
	assert.Empty(t, listComputeServers(t, compute, "SHUTOFF"))
	detail, err = servers.Get(ctx, compute, fixtureServerID).Extract()
	require.NoError(t, err)
	check(*detail, "ACTIVE", "active", servers.RUNNING)
	retained, err := volumeattach.Get(ctx, compute, fixtureServerID, fixtureVolumeID).Extract()
	require.NoError(t, err)
	assert.Equal(t, attachment, retained)
	err = servers.Start(ctx, compute, fixtureServerID).ExtractErr()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusConflict), "repeat start: %v", err)
}

func TestNovaPowerActionsWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	projectID := routerProjectID(t, provider)
	base := computePath + "/" + projectID + "/servers/"
	path := base + fixtureServerID
	action := path + "/action"
	const stop = `{"os-stop":null}`
	const start = `{"os-start":null}`
	const missing = "00000000-0000-0000-0000-000000000000"
	auth := map[string]string{"X-Auth-Token": provider.Token(), "OpenStack-API-Version": "compute 2.1"}
	checkFault := func(wantStatus int, kind string, status int, headers http.Header, body []byte) {
		t.Helper()
		require.Equal(t, wantStatus, status, "%s", body)
		requireJSON(t, headers)
		var faults map[string]struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		}
		require.NoError(t, json.Unmarshal(body, &faults))
		require.Contains(t, faults, kind)
		assert.Equal(t, wantStatus, faults[kind].Code)
		assert.NotEmpty(t, faults[kind].Message)
	}
	for _, rejected := range []struct {
		name   string
		path   string
		body   string
		header map[string]string
		status int
		kind   string
	}{
		{"anonymous", action, stop, nil, http.StatusUnauthorized, "unauthorized"},
		{"missing server", base + missing + "/action", stop, auth, http.StatusNotFound, "itemNotFound"},
		{"wrong URL project", computePath + "/another-project/servers/" + fixtureServerID + "/action", stop, auth,
			http.StatusBadRequest, "badRequest"},
		{"malformed microversion", action, stop,
			map[string]string{"X-Auth-Token": provider.Token(), "OpenStack-API-Version": "compute bad"},
			http.StatusBadRequest, "badRequest"},
		{"unsupported microversion", action, stop,
			map[string]string{"X-Auth-Token": provider.Token(), "OpenStack-API-Version": "compute 2.2"},
			http.StatusNotAcceptable, "computeFault"},
		{"empty action", action, `{}`, auth, http.StatusBadRequest, "badRequest"},
		{"malformed action", action, `{"os-stop":`, auth, http.StatusBadRequest, "badRequest"},
		{"unknown action", action, `{"os-other":null}`, auth, http.StatusBadRequest, "badRequest"},
		{"duplicate action", action, `{"os-stop":null,"os-stop":null}`, auth, http.StatusBadRequest, "badRequest"},
		{"mixed actions", action, `{"os-stop":null,"os-start":null}`, auth, http.StatusBadRequest, "badRequest"},
	} {
		t.Run(rejected.name, func(t *testing.T) {
			status, headers, body := request(t, instance, http.MethodPost, rejected.path, rejected.body, rejected.header)
			checkFault(rejected.status, rejected.kind, status, headers, body)
		})
	}
	status, headers, body := request(t, instance, http.MethodGet, path, "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	require.Equal(t, "ACTIVE", decodeWireComputeServer(t, body).Status)

	status, _, body = request(t, instance, http.MethodPut, faultPath, "", map[string]string{
		"X-SandStack-Admin-Token": adminToken,
	})
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	status, headers, body = request(t, instance, http.MethodGet, path, "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	failed := decodeWireComputeServer(t, body)
	require.Equal(t, "ERROR", failed.Status)
	require.NotEmpty(t, failed.Fault)
	status, headers, body = request(t, instance, http.MethodPost, action, start, auth)
	checkFault(http.StatusConflict, "conflictingRequest", status, headers, body)

	// https://docs.openstack.org/api-ref/compute/#stop-server-os-stop-action
	status, headers, body = request(t, instance, http.MethodPost, action, stop, auth)
	require.Equal(t, http.StatusAccepted, status, "%s", body)
	assert.Empty(t, body)
	assert.Equal(t, "compute 2.1", headers.Get("OpenStack-API-Version"))
	assert.Equal(t, "2.1", headers.Get("X-OpenStack-Nova-API-Version"))
	status, headers, body = request(t, instance, http.MethodGet, path, "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	stopped := decodeWireComputeServer(t, body)
	assert.Equal(t, "SHUTOFF", stopped.Status)
	assert.Equal(t, "stopped", stopped.VMState)
	require.NotNil(t, stopped.PowerState)
	assert.Equal(t, 4, *stopped.PowerState)
	assert.JSONEq(t, "null", string(stopped.TaskState))
	assert.Empty(t, stopped.Fault)

	// https://docs.openstack.org/api-ref/compute/#start-server-os-start-action
	projectless := computePath + "/servers/" + fixtureServerID + "/action"
	status, headers, body = request(t, instance, http.MethodPost, projectless, `{"os-start":{}}`, auth)
	require.Equal(t, http.StatusAccepted, status, "%s", body)
	assert.Empty(t, body)
	assert.Equal(t, "2.1", headers.Get("X-OpenStack-Nova-API-Version"))
	status, headers, body = request(t, instance, http.MethodGet, path, "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	active := decodeWireComputeServer(t, body)
	assert.Equal(t, "ACTIVE", active.Status)
	assert.Equal(t, "active", active.VMState)
	require.NotNil(t, active.PowerState)
	assert.Equal(t, 1, *active.PowerState)
	assert.JSONEq(t, "null", string(active.TaskState))
	assert.Empty(t, active.Fault)
}
