package server_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/instanceactions"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/servers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/servers.py#L1239-L1423
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L3694-L3913
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/manager.py#L4082-L4094
func TestGophercloudRebuildsServerFromSnapshot(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	clients := newBootClients(t, instance)
	imageClient := newSnapshotImageClient(t, provider)
	ctx := t.Context()
	prepareBootNetwork(t, clients.network)
	serverID := bootImageServer(t, clients.compute, "web", fixtureImageID)
	imageID, err := servers.CreateImage(ctx, clients.compute, serverID, servers.CreateImageOpts{
		Name: "web-snap",
	}).ExtractImageID()
	require.NoError(t, err)
	waitForImage(t, imageClient, imageID)

	rebuilt, err := servers.Rebuild(ctx, clients.compute, serverID, servers.RebuildOpts{
		ImageRef:  imageID,
		Name:      "web-rebuilt",
		AdminPass: "chosen-password",
		Metadata:  map[string]string{"role": "rebuilt"},
	}).Extract()

	require.NoError(t, err)
	assert.Equal(t, serverID, rebuilt.ID)
	assert.Equal(t, "web-rebuilt", rebuilt.Name)
	assert.Equal(t, imageID, rebuilt.Image["id"])
	assert.Equal(t, map[string]string{"role": "rebuilt"}, rebuilt.Metadata)
	assert.Equal(t, "chosen-password", rebuilt.AdminPass)
	waitForServer(t, clients.compute, serverID, "ACTIVE")
	server, err := servers.Get(ctx, clients.compute, serverID).Extract()
	require.NoError(t, err)
	assert.Equal(t, imageID, server.Image["id"])
	assert.Equal(t, "web-rebuilt", server.Name)
	assert.Equal(t, "active", server.VmState)
	assert.Empty(t, server.TaskState)
	pages, err := instanceactions.List(clients.compute, serverID, nil).AllPages(ctx)
	require.NoError(t, err)
	actions, err := instanceactions.ExtractInstanceActions(pages)
	require.NoError(t, err)
	require.NotEmpty(t, actions)
	assert.Equal(t, "rebuild", actions[0].Action)
}

func TestNovaRebuildWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	clients := newBootClients(t, instance)
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	prepareBootNetwork(t, clients.network)
	serverID := bootImageServer(t, clients.compute, "web", fixtureImageID)
	action := func(id, body string) (int, http.Header, []byte) {
		t.Helper()
		return request(t, instance, http.MethodPost, computePath+"/servers/"+id+"/action", body, auth)
	}
	faultMessage := func(kind string, body []byte) string {
		t.Helper()
		var faults map[string]struct {
			Message string `json:"message"`
		}
		require.NoError(t, json.Unmarshal(body, &faults))
		return faults[kind].Message
	}
	missingImage := "10000000-0000-4000-8000-999999999999"

	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/schemas/servers.py#L408-L430
	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/validation/parameter_types.py#L362-L365
	for _, body := range []string{
		`{"rebuild":{}}`,
		`{"rebuild":{"imageRef":""}}`,
		`{"rebuild":{"imageRef":"not-a-uuid"}}`,
		`{"rebuild":{"imageRef":"` + fixtureImageID + `","adminPass":null}}`,
		`{"rebuild":{"imageRef":"` + fixtureImageID + `","name":" web"}}`,
		`{"rebuild":{"imageRef":"` + fixtureImageID + `","metadata":{"key":1}}}`,
		`{"rebuild":{"imageRef":"` + fixtureImageID + `","OS-DCF:diskConfig":"auto"}}`,
		`{"rebuild":{"imageRef":"` + fixtureImageID + `","extra":true}}`,
		`{"rebuild":null}`,
	} {
		status, headers, response := action(serverID, body)
		requireNovaFault(t, http.StatusBadRequest, "badRequest", status, headers, response)
	}
	status, headers, body := action(serverID, `{"rebuild":{"name":"web"}}`)
	requireNovaFault(t, http.StatusBadRequest, "badRequest", status, headers, body)
	assert.Equal(t, "'imageRef' is a required property.", faultMessage("badRequest", body))

	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/servers.py#L1338-L1371
	status, headers, body = action(serverID, `{"rebuild":{"imageRef":"`+missingImage+`"}}`)
	requireNovaFault(t, http.StatusBadRequest, "badRequest", status, headers, body)
	assert.Equal(t, "Cannot find image for rebuild", faultMessage("badRequest", body))

	status, headers, body = action("00000000-0000-4000-8000-000000000000",
		`{"rebuild":{"imageRef":"`+fixtureImageID+`"}}`)
	requireNovaFault(t, http.StatusNotFound, "itemNotFound", status, headers, body)

	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L3696-L3700
	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/common.py#L330-L346
	require.Equal(t, http.StatusAccepted, responseStatus(action(serverID, `{"pause":null}`)))
	status, headers, body = action(serverID, `{"rebuild":{"imageRef":"`+fixtureImageID+`"}}`)
	requireNovaFault(t, http.StatusConflict, "conflictingRequest", status, headers, body)
	assert.Equal(t, "Cannot 'rebuild' instance "+serverID+" while it is in vm_state paused",
		faultMessage("conflictingRequest", body))
	require.Equal(t, http.StatusAccepted, responseStatus(action(serverID, `{"unpause":null}`)))

	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L247-L253
	require.Equal(t, http.StatusAccepted, responseStatus(action(serverID, `{"lock":null}`)))
	status, headers, body = action(serverID, `{"rebuild":{"imageRef":"`+fixtureImageID+`"}}`)
	requireNovaFault(t, http.StatusConflict, "conflictingRequest", status, headers, body)
	assert.Equal(t, "Instance "+serverID+" is locked", faultMessage("conflictingRequest", body))
	require.Equal(t, http.StatusAccepted, responseStatus(action(serverID, `{"unlock":null}`)))

	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/servers.py#L1373-L1423
	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/schemas/servers.py#L1798-L1919
	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/conf/api.py#L351-L358
	require.Equal(t, http.StatusAccepted, responseStatus(action(serverID, `{"os-stop":null}`)))
	status, headers, body = action(serverID,
		`{"rebuild":{"imageRef":"`+fixtureImageID+`","OS-DCF:diskConfig":"AUTO"}}`)

	require.Equal(t, http.StatusAccepted, status, "%s", body)
	requireJSON(t, headers)
	var response struct {
		Server map[string]json.RawMessage `json:"server"`
	}
	require.NoError(t, json.Unmarshal(body, &response))
	for _, field := range []string{
		"accessIPv4", "accessIPv6", "addresses", "adminPass", "created", "flavor", "hostId", "id", "image",
		"links", "metadata", "name", "progress", "status", "tenant_id", "updated", "user_id", "OS-DCF:diskConfig",
	} {
		assert.Contains(t, response.Server, field)
	}
	var server struct {
		Status     string `json:"status"`
		AdminPass  string `json:"adminPass"`
		DiskConfig string `json:"OS-DCF:diskConfig"`
		Progress   int    `json:"progress"`
		Links      []struct {
			Rel  string `json:"rel"`
			Href string `json:"href"`
		} `json:"links"`
	}
	require.NoError(t, json.Unmarshal(body, &struct {
		Server any `json:"server"`
	}{Server: &server}))
	assert.Equal(t, "SHUTOFF", server.Status)
	assert.Len(t, server.AdminPass, 12)
	assert.Equal(t, "AUTO", server.DiskConfig)
	assert.Equal(t, 0, server.Progress)
	require.NotEmpty(t, server.Links)
	assert.Equal(t, server.Links[0].Href, headers.Get("Location"))
	requestID := headers.Get("X-Openstack-Request-Id")
	status, _, body = request(t, instance, http.MethodGet,
		computePath+"/servers/"+serverID+"/os-instance-actions/"+requestID, "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	assert.Contains(t, string(body), `"action":"rebuild"`)
}
