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

func TestGophercloudLocksAndUnlocksNovaServer(t *testing.T) {
	instance := startServer(t, "")
	newCompute := func() *gophercloud.ServiceClient {
		t.Helper()
		client, err := openstack.NewComputeV2(authenticate(t, instance), gophercloud.EndpointOpts{
			Region: "RegionOne", Availability: gophercloud.AvailabilityPublic,
		})
		require.NoError(t, err)
		return client
	}
	compute := newCompute()
	ctx := t.Context()
	conflict := func(err error, action string) {
		t.Helper()
		assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusConflict), "%s: %v", action, err)
	}
	_, err := volumeattach.Create(ctx, compute, fixtureServerID, volumeattach.CreateOpts{
		VolumeID: fixtureVolumeID,
	}).Extract()
	require.NoError(t, err)

	require.NoError(t, servers.Lock(ctx, compute, fixtureServerID).ExtractErr())

	require.NoError(t, servers.Lock(ctx, compute, fixtureServerID).ExtractErr())
	conflict(servers.Stop(ctx, compute, fixtureServerID).ExtractErr(), "stop locked server")
	conflict(servers.Pause(ctx, compute, fixtureServerID).ExtractErr(), "pause locked server")
	conflict(servers.Suspend(ctx, compute, fixtureServerID).ExtractErr(), "suspend locked server")
	hard := servers.RebootOpts{Type: servers.HardReboot}
	conflict(servers.Reboot(ctx, compute, fixtureServerID, hard).ExtractErr(), "reboot locked server")
	_, err = servers.UpdateMetadata(ctx, compute, fixtureServerID, servers.MetadataOpts{"locked": "yes"}).Extract()
	conflict(err, "merge metadata on locked server")
	err = volumeattach.Delete(ctx, compute, fixtureServerID, fixtureVolumeID).ExtractErr()
	conflict(err, "detach from locked server")
	renamed, err := servers.Update(ctx, compute, fixtureServerID, servers.UpdateOpts{Name: "locked-server"}).Extract()
	require.NoError(t, err)
	assert.Equal(t, "locked-server", renamed.Name)
	detail, err := servers.Get(ctx, compute, fixtureServerID).Extract()
	require.NoError(t, err)
	assert.Equal(t, "ACTIVE", detail.Status)
	assert.Nil(t, detail.Locked)
	assert.Equal(t, []servers.AttachedVolume{{ID: fixtureVolumeID}}, detail.AttachedVolumes)

	require.NoError(t, servers.Unlock(ctx, compute, fixtureServerID).ExtractErr())

	require.NoError(t, servers.Unlock(ctx, compute, fixtureServerID).ExtractErr())
	require.NoError(t, servers.Stop(ctx, compute, fixtureServerID).ExtractErr())
	stopped := listComputeServers(t, compute, "SHUTOFF")
	require.Len(t, stopped, 1)
	assert.Equal(t, fixtureServerID, stopped[0].ID)

	require.NoError(t, servers.Lock(ctx, compute, fixtureServerID).ExtractErr())
	status, _, body := request(t, instance, http.MethodPost, "/_sandstack/reset", "",
		map[string]string{"X-SandStack-Admin-Token": adminToken})
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	compute = newCompute()

	require.NoError(t, servers.Pause(ctx, compute, fixtureServerID).ExtractErr())
	paused := listComputeServers(t, compute, "PAUSED")
	require.Len(t, paused, 1)
	assert.Equal(t, "fixture-server", paused[0].Name)
}

func TestNovaLockActionsWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	projectPath := computePath + "/" + routerProjectID(t, provider)
	path := projectPath + "/servers/" + fixtureServerID
	action := path + "/action"
	attachments := projectPath + fixtureAttachmentPath
	auth := map[string]string{"X-Auth-Token": provider.Token(), "OpenStack-API-Version": "compute 2.1"}
	read := func() (wireComputeServer, map[string]json.RawMessage) {
		t.Helper()
		status, headers, body := request(t, instance, http.MethodGet, path, "", auth)
		require.Equal(t, http.StatusOK, status, "%s", body)
		requireJSON(t, headers)
		var fields struct {
			Server map[string]json.RawMessage `json:"server"`
		}
		require.NoError(t, json.Unmarshal(body, &fields))
		return decodeWireComputeServer(t, body), fields.Server
	}

	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/schemas/lock_server.py#L13-L47
	for _, body := range []string{
		`{"lock":null,"unlock":null}`,
		`{"lock":null,"other":true}`,
		`{"unlock":null,"unlock":null}`,
	} {
		status, headers, response := request(t, instance, http.MethodPost, action, body, auth)
		requireNovaFault(t, http.StatusBadRequest, "badRequest", status, headers, response)
	}
	const missing = "00000000-0000-0000-0000-000000000000"
	status, headers, body := request(t, instance, http.MethodPost,
		computePath+"/servers/"+missing+"/action", `{"lock":null}`, auth)
	requireNovaFault(t, http.StatusNotFound, "itemNotFound", status, headers, body)

	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/lock_server.py#L31-L50
	status, headers, body = request(t, instance, http.MethodPost, action,
		`{"lock":{"locked_reason":"ignored before 2.73"}}`, auth)

	require.Equal(t, http.StatusAccepted, status, "%s", body)
	assert.Empty(t, body)
	assert.Equal(t, "compute 2.1", headers.Get("OpenStack-API-Version"))
	locked, fields := read()
	assert.Equal(t, "ACTIVE", locked.Status)
	assert.NotContains(t, fields, "locked")
	assert.NotContains(t, fields, "locked_reason")

	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L247-L253
	for _, test := range []struct {
		method, path, body string
	}{
		{http.MethodPost, action, `{"os-stop":null}`},
		{http.MethodPost, action, `{"reboot":{"type":"HARD"}}`},
		{http.MethodPost, action, `{"pause":null}`},
		{http.MethodPost, action, `{"resume":null}`},
		{http.MethodPut, path + "/metadata", `{"metadata":{}}`},
		{http.MethodPut, path + "/metadata/item", `{"meta":{"item":"value"}}`},
		{http.MethodPost, attachments, `{"volumeAttachment":{"volumeId":"` + fixtureVolumeID + `"}}`},
	} {
		status, headers, body = request(t, instance, test.method, test.path, test.body, auth)
		requireNovaFault(t, http.StatusConflict, "conflictingRequest", status, headers, body)
	}

	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/server_metadata.py#L147-L169
	status, headers, body = request(t, instance, http.MethodDelete, path+"/metadata/missing", "", auth)

	requireNovaFault(t, http.StatusNotFound, "itemNotFound", status, headers, body)

	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/volume_attachments.py#L394-L440
	status, headers, body = request(t, instance, http.MethodDelete, attachments+"/"+fixtureVolumeID, "", auth)

	requireNovaFault(t, http.StatusNotFound, "itemNotFound", status, headers, body)
	status, _, body = request(t, instance, http.MethodGet, attachments, "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	assert.JSONEq(t, `{"volumeAttachments":[]}`, string(body))

	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L5046-L5094
	status, _, body = request(t, instance, http.MethodPost, action, `{"lock":"again"}`, auth)

	require.Equal(t, http.StatusAccepted, status, "%s", body)
	assert.Empty(t, body)

	projectless := computePath + "/servers/" + fixtureServerID + "/action"
	status, headers, body = request(t, instance, http.MethodPost, projectless, `{"unlock":{}}`, auth)

	require.Equal(t, http.StatusAccepted, status, "%s", body)
	assert.Empty(t, body)
	assert.Equal(t, "2.1", headers.Get("X-OpenStack-Nova-API-Version"))
	unlocked, fields := read()
	assert.Equal(t, "ACTIVE", unlocked.Status)
	assert.NotContains(t, fields, "locked")
	status, _, body = request(t, instance, http.MethodPost, action, `{"pause":null}`, auth)
	require.Equal(t, http.StatusAccepted, status, "%s", body)
	paused, _ := read()
	assert.Equal(t, "PAUSED", paused.Status)
}
