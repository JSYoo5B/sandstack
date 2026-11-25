package server_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/servers"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/volumeattach"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGophercloudRenamesStoppedNovaServerWithoutChangingAttachments(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	compute, err := openstack.NewComputeV2(provider, gophercloud.EndpointOpts{
		Region: "RegionOne", Availability: gophercloud.AvailabilityPublic,
	})
	require.NoError(t, err)
	ctx := t.Context()
	attached, err := volumeattach.Create(ctx, compute, fixtureServerID, volumeattach.CreateOpts{
		VolumeID: fixtureVolumeID,
	}).Extract()
	require.NoError(t, err)
	require.NoError(t, servers.Stop(ctx, compute, fixtureServerID).ExtractErr())
	before, err := servers.Get(ctx, compute, fixtureServerID).Extract()
	require.NoError(t, err)
	require.Equal(t, "SHUTOFF", before.Status)
	require.Equal(t, []servers.AttachedVolume{{ID: fixtureVolumeID}}, before.AttachedVolumes)
	status, _, body := request(t, instance, http.MethodPut, unavailableControl, "",
		map[string]string{"X-SandStack-Admin-Token": adminToken})
	require.Equal(t, http.StatusNoContent, status, "%s", body)

	const newName = "retained attachment 서버"
	result, err := servers.Update(ctx, compute, fixtureServerID, servers.UpdateOpts{Name: newName}).Extract()

	require.NoError(t, err)
	assert.Equal(t, fixtureServerID, result.ID)
	assert.Equal(t, newName, result.Name)
	assert.Equal(t, before.Status, result.Status)
	assert.Equal(t, before.Created, result.Created)
	assert.False(t, result.Updated.Before(before.Updated))
	assert.Empty(t, result.VmState)
	assert.Empty(t, result.AttachedVolumes)
	want := *before
	want.Name, want.Updated = newName, result.Updated
	assert.Equal(t, []servers.Server{want}, listComputeServers(t, compute, "SHUTOFF"))

	_, err = servers.Get(ctx, compute, fixtureServerID).Extract()
	require.True(t, gophercloud.ResponseCodeIs(err, http.StatusServiceUnavailable), "one-shot detail fault remains pending: %v", err)
	recovered, err := servers.Get(ctx, compute, fixtureServerID).Extract()
	require.NoError(t, err)
	assert.Equal(t, &want, recovered)
	association, err := volumeattach.Get(ctx, compute, fixtureServerID, fixtureVolumeID).Extract()
	require.NoError(t, err)
	assert.Equal(t, attached, association)
}

func TestNovaNameUpdateWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	projectID := routerProjectID(t, provider)
	collection := computePath + "/" + projectID + "/servers/"
	path := collection + fixtureServerID
	auth := map[string]string{"X-Auth-Token": provider.Token(), "OpenStack-API-Version": "compute 2.1"}
	read := func(body []byte) map[string]json.RawMessage {
		t.Helper()
		var response struct {
			Server map[string]json.RawMessage `json:"server"`
		}
		require.NoError(t, json.Unmarshal(body, &response))
		assert.JSONEq(t, `"`+fixtureServerID+`"`, string(response.Server["id"]))
		return response.Server
	}
	status, headers, body := request(t, instance, http.MethodGet, path, "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	baseline := string(body)
	const rename = `{"server":{"name":"wire-name"}}`
	status, _, body = request(t, instance, http.MethodPut, path, rename, nil)
	require.Equal(t, http.StatusUnauthorized, status, "%s", body)

	atLimit := strings.Repeat("한", 255)
	for _, invalid := range []string{
		`{}`, `{"server":null}`, `{"server":{"name":null}}`, `{"server":{"name":""}}`,
		`{"server":{"name":42}}`,
		`{"server":{"name":" leading"}}`, `{"server":{"name":"trailing "}}`,
		`{"server":{"name":"one\u0007two"}}`,
		`{"server":{"name":"outside BMP 😀"}}`,
		`{"server":{"name":"cannot apply","name":"duplicate"}}`,
		`{"server":{"name":"cannot apply","accessIPv4":"192.0.2.1"}}`,
		fmt.Sprintf(`{"server":{"name":%q}}`, atLimit+"한"),
	} {
		status, headers, body := request(t, instance, http.MethodPut, path, invalid, auth)
		require.Equal(t, http.StatusBadRequest, status, "%q: %s", invalid, body)
		requireJSON(t, headers)
	}
	status, headers, body = request(t, instance, http.MethodGet, path, "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	assert.JSONEq(t, baseline, string(body))

	// https://docs.openstack.org/api-ref/compute/#update-server
	// https://github.com/openstack/nova/blob/master/nova/api/openstack/compute/servers.py#L965-L994
	status, headers, body = request(t, instance, http.MethodPut, path, fmt.Sprintf(`{"server":{"name":%q}}`, atLimit), auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	assert.Equal(t, "compute 2.1", headers.Get("OpenStack-API-Version"))
	assert.Equal(t, "2.1", headers.Get("X-OpenStack-Nova-API-Version"))
	updated := read(body)
	name, err := json.Marshal(atLimit)
	require.NoError(t, err)
	assert.JSONEq(t, string(name), string(updated["name"]))
	assert.JSONEq(t, `"`+projectID+`"`, string(updated["tenant_id"]))
	assert.JSONEq(t, `"ACTIVE"`, string(updated["status"]))
	assert.JSONEq(t, `"MANUAL"`, string(updated["OS-DCF:diskConfig"]))
	assert.JSONEq(t, "0", string(updated["progress"]))
	for _, field := range []string{"hostId", "accessIPv4", "accessIPv6"} {
		assert.JSONEq(t, `""`, string(updated[field]), "%s", field)
	}
	extensions := []string{
		"OS-EXT-STS:vm_state", "OS-EXT-STS:power_state", "OS-EXT-STS:task_state",
		"os-extended-volumes:volumes_attached",
	}
	for _, field := range extensions {
		assert.NotContains(t, updated, field)
	}
	updatedBody := string(body)
	status, headers, body = request(t, instance, http.MethodPut, path, `{"server":{}}`, auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	assert.JSONEq(t, updatedBody, string(body))
	status, headers, body = request(t, instance, http.MethodGet, path, "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	detail := read(body)
	for _, field := range []string{"id", "name", "status", "updated", "tenant_id", "user_id", "links", "metadata", "addresses"} {
		assert.Equal(t, updated[field], detail[field], "%s", field)
	}
	for _, field := range extensions {
		assert.Contains(t, detail, field)
	}

	const missing = "00000000-0000-0000-0000-000000000000"
	status, headers, body = request(t, instance, http.MethodPut, collection+missing, rename, auth)
	require.Equal(t, http.StatusNotFound, status, "%s", body)
	requireJSON(t, headers)
	var failure map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(body, &failure))
	assert.Contains(t, failure, "itemNotFound")

	status, _, body = request(t, instance, http.MethodPost, path+"/action", `{"os-stop":null}`, auth)
	require.Equal(t, http.StatusAccepted, status, "%s", body)
	status, headers, body = request(t, instance, http.MethodPut, path, rename, auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	stopped := read(body)
	assert.JSONEq(t, `"SHUTOFF"`, string(stopped["status"]))
	assert.NotContains(t, stopped, "progress")
}
