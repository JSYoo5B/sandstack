package server_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/servers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGophercloudRebootsNovaServer(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	compute, err := openstack.NewComputeV2(provider, gophercloud.EndpointOpts{
		Region: "RegionOne", Availability: gophercloud.AvailabilityPublic,
	})
	require.NoError(t, err)
	ctx := t.Context()
	admin := map[string]string{"X-SandStack-Admin-Token": adminToken}
	read := func() *servers.Server {
		t.Helper()
		server, err := servers.Get(ctx, compute, fixtureServerID).Extract()
		require.NoError(t, err)
		return server
	}
	reboot := func(method servers.RebootMethod) error {
		t.Helper()
		return servers.Reboot(ctx, compute, fixtureServerID, servers.RebootOpts{Type: method}).ExtractErr()
	}
	control := func(path string) {
		t.Helper()
		status, _, body := request(t, instance, http.MethodPut, path, "", admin)
		require.Equal(t, http.StatusNoContent, status, "%s", body)
	}
	assertActive := func(server *servers.Server) {
		t.Helper()
		assert.Equal(t, fixtureServerID, server.ID)
		assert.Equal(t, "ACTIVE", server.Status)
		assert.Equal(t, "active", server.VmState)
		assert.Equal(t, servers.PowerState(servers.RUNNING), server.PowerState)
		assert.Empty(t, server.Fault)
	}

	require.NoError(t, servers.Stop(ctx, compute, fixtureServerID).ExtractErr())
	err = reboot(servers.SoftReboot)
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusConflict), "soft reboot of SHUTOFF: %v", err)
	stopped := listComputeServers(t, compute, "SHUTOFF")
	require.Len(t, stopped, 1)
	assert.Equal(t, fixtureServerID, stopped[0].ID)

	control(unavailableControl)
	require.NoError(t, reboot(servers.HardReboot))

	active := listComputeServers(t, compute, "ACTIVE")
	require.Len(t, active, 1)
	assertActive(&active[0])
	assert.Empty(t, listComputeServers(t, compute, "SHUTOFF"))
	_, err = servers.Get(ctx, compute, fixtureServerID).Extract()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusServiceUnavailable), "reboot leaves detail failure pending: %v", err)
	assertActive(read())

	require.NoError(t, reboot(servers.SoftReboot))
	assertActive(read())

	control(faultPath)
	failed := read()
	require.Equal(t, "ERROR", failed.Status)
	require.NotEmpty(t, failed.Fault)
	err = reboot(servers.SoftReboot)
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusConflict), "soft reboot of ERROR: %v", err)
	assert.Equal(t, failed, read())

	require.NoError(t, reboot(servers.HardReboot))
	assertActive(read())
	assert.Empty(t, listComputeServers(t, compute, "ERROR"))
}

func TestNovaRebootWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	collection := computePath + "/" + routerProjectID(t, provider) + "/servers/"
	action := collection + fixtureServerID + "/action"
	auth := map[string]string{"X-Auth-Token": provider.Token(), "OpenStack-API-Version": "compute 2.1"}

	// https://github.com/openstack/nova/blob/master/nova/api/openstack/compute/schemas/servers.py#L495-L512
	for _, body := range []string{
		`{"reboot":null}`,
		`{"reboot":{}}`,
		`{"reboot":{"type":5}}`,
		`{"reboot":{"type":"HaRd"}}`,
		`{"reboot":{"type":"warm"}}`,
		`{"reboot":{"type":"SOFT","other":true}}`,
	} {
		status, headers, response := request(t, instance, http.MethodPost, action, body, auth)
		assert.Equal(t, http.StatusBadRequest, status, "%s: %s", body, response)
		requireJSON(t, headers)
	}

	const missingServerID = "00000000-0000-0000-0000-000000000000"
	status, headers, response := request(t, instance, http.MethodPost, collection+missingServerID+"/action",
		`{"reboot":{"type":"HARD"}}`, auth)
	assert.Equal(t, http.StatusNotFound, status, "%s", response)
	requireJSON(t, headers)

	for _, rebootType := range []string{"HARD", "Hard", "hard", "SOFT", "Soft", "soft"} {
		status, headers, response := request(t, instance, http.MethodPost, action,
			fmt.Sprintf(`{"reboot":{"type":%q}}`, rebootType), auth)
		assert.Equal(t, http.StatusAccepted, status, "%s: %s", rebootType, response)
		assert.Equal(t, "compute 2.1", headers.Get("OpenStack-API-Version"))
		assert.Empty(t, response)
	}
}
