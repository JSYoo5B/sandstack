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

// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/servers.py#L1081-L1103
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/servers.py#L1149-L1216
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/common.py#L76-L83
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/manager.py#L6482-L6488
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/manager.py#L5112-L5130
func TestGophercloudResizesServerAndConfirms(t *testing.T) {
	instance := startServer(t, "")
	clients := newBootClients(t, instance)
	ctx := t.Context()
	prepareBootNetwork(t, clients.network)
	serverID := bootImageServer(t, clients.compute, "web", fixtureImageID)

	resizeErr := servers.Resize(ctx, clients.compute, serverID, servers.ResizeOpts{FlavorRef: "2"}).ExtractErr()

	require.NoError(t, resizeErr)
	waitForServer(t, clients.compute, serverID, "VERIFY_RESIZE")
	resized, err := servers.Get(ctx, clients.compute, serverID).Extract()
	require.NoError(t, err)
	assert.Equal(t, "2", resized.Flavor["id"])
	assert.Equal(t, "resized", resized.VmState)
	assert.Equal(t, servers.PowerState(servers.RUNNING), resized.PowerState)
	assert.Empty(t, resized.TaskState)

	confirmErr := servers.ConfirmResize(ctx, clients.compute, serverID).ExtractErr()

	require.NoError(t, confirmErr)
	waitForServer(t, clients.compute, serverID, "ACTIVE")
	confirmed, err := servers.Get(ctx, clients.compute, serverID).Extract()
	require.NoError(t, err)
	assert.Equal(t, "2", confirmed.Flavor["id"])
	assert.Equal(t, "active", confirmed.VmState)
	pages, err := instanceactions.List(clients.compute, serverID, nil).AllPages(ctx)
	require.NoError(t, err)
	actions, err := instanceactions.ExtractInstanceActions(pages)
	require.NoError(t, err)
	require.Len(t, actions, 3)
	assert.Equal(t, "confirmResize", actions[0].Action)
	assert.Equal(t, "resize", actions[1].Action)
}

// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/servers.py#L1105-L1127
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L4019-L4107
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/manager.py#L6526-L6581
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/manager.py#L5755-L5790
func TestGophercloudResizesStoppedServerAndReverts(t *testing.T) {
	instance := startServer(t, "")
	clients := newBootClients(t, instance)
	ctx := t.Context()
	prepareBootNetwork(t, clients.network)
	serverID := bootImageServer(t, clients.compute, "web", fixtureImageID)
	require.NoError(t, servers.Stop(ctx, clients.compute, serverID).ExtractErr())
	waitForServer(t, clients.compute, serverID, "SHUTOFF")

	resizeErr := servers.Resize(ctx, clients.compute, serverID, servers.ResizeOpts{
		FlavorRef: "2", DiskConfig: servers.Manual,
	}).ExtractErr()

	require.NoError(t, resizeErr)
	waitForServer(t, clients.compute, serverID, "VERIFY_RESIZE")
	resized, err := servers.Get(ctx, clients.compute, serverID).Extract()
	require.NoError(t, err)
	assert.Equal(t, "2", resized.Flavor["id"])
	assert.Equal(t, servers.PowerState(servers.SHUTDOWN), resized.PowerState)
	stopErr := servers.Stop(ctx, clients.compute, serverID).ExtractErr()
	assert.Error(t, stopErr)

	revertErr := servers.RevertResize(ctx, clients.compute, serverID).ExtractErr()

	require.NoError(t, revertErr)
	waitForServer(t, clients.compute, serverID, "SHUTOFF")
	reverted, err := servers.Get(ctx, clients.compute, serverID).Extract()
	require.NoError(t, err)
	assert.Equal(t, "1", reverted.Flavor["id"])
	assert.Equal(t, "stopped", reverted.VmState)
	require.NoError(t, servers.Start(ctx, clients.compute, serverID).ExtractErr())
	waitForServer(t, clients.compute, serverID, "ACTIVE")
}

func TestNovaResizeWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	clients := newBootClients(t, instance)
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	prepareBootNetwork(t, clients.network)
	serverID := bootImageServer(t, clients.compute, "web", fixtureImageID)
	action := func(body string) (int, http.Header, []byte) {
		t.Helper()
		return request(t, instance, http.MethodPost, computePath+"/servers/"+serverID+"/action", body, auth)
	}
	detail := func() map[string]any {
		t.Helper()
		status, _, body := request(t, instance, http.MethodGet, computePath+"/servers/"+serverID, "", auth)
		require.Equal(t, http.StatusOK, status, "%s", body)
		var response struct {
			Server map[string]any `json:"server"`
		}
		require.NoError(t, json.Unmarshal(body, &response))
		return response.Server
	}
	faultMessage := func(kind string, body []byte) string {
		t.Helper()
		var faults map[string]struct {
			Message string `json:"message"`
		}
		require.NoError(t, json.Unmarshal(body, &faults))
		return faults[kind].Message
	}

	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/schemas/servers.py#L467-L482
	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/validation/parameter_types.py#L427-L429
	for _, body := range []string{
		`{"resize":{}}`,
		`{"resize":{"flavorRef":""}}`,
		`{"resize":{"flavorRef":2.5}}`,
		`{"resize":{"flavorRef":true}}`,
		`{"resize":{"flavorRef":"2","OS-DCF:diskConfig":"auto"}}`,
		`{"resize":{"flavorRef":"2","extra":true}}`,
		`{"resize":"2"}`,
	} {
		status, headers, response := action(body)
		requireNovaFault(t, http.StatusBadRequest, "badRequest", status, headers, response)
	}

	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/servers.py#L1155-L1157
	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L4357-L4394
	for _, tt := range []struct {
		body    string
		message string
	}{
		{body: `{"resize":{"flavorRef":"999"}}`, message: "Flavor 999 could not be found."},
		{
			body:    `{"resize":{"flavorRef":"http://localhost/flavors/2"}}`,
			message: "Flavor http://localhost/flavors/2 could not be found.",
		},
		{body: `{"resize":{"flavorRef":1}}`, message: "When resizing, instances must change flavor!"},
	} {
		status, headers, body := action(tt.body)
		requireNovaFault(t, http.StatusBadRequest, "badRequest", status, headers, body)
		assert.Equal(t, tt.message, faultMessage("badRequest", body))
	}

	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/servers.py#L1081-L1103
	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L4130-L4131
	status, headers, body := action(`{"confirmResize":null}`)
	requireNovaFault(t, http.StatusConflict, "conflictingRequest", status, headers, body)
	assert.Equal(t, "Cannot 'confirmResize' instance "+serverID+" while it is in vm_state active",
		faultMessage("conflictingRequest", body))
	status, headers, body = action(`{"revertResize":null}`)
	requireNovaFault(t, http.StatusConflict, "conflictingRequest", status, headers, body)
	assert.Equal(t, "Cannot 'revertResize' instance "+serverID+" while it is in vm_state active",
		faultMessage("conflictingRequest", body))

	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L247-L253
	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/exception.py#L1484-L1485
	require.Equal(t, http.StatusAccepted, responseStatus(action(`{"lock":null}`)))
	status, headers, body = action(`{"resize":{"flavorRef":"2"}}`)
	requireNovaFault(t, http.StatusConflict, "conflictingRequest", status, headers, body)
	assert.Equal(t, "Instance "+serverID+" is locked", faultMessage("conflictingRequest", body))
	require.Equal(t, http.StatusAccepted, responseStatus(action(`{"unlock":null}`)))

	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/servers.py#L1149-L1157
	status, headers, body = action(`{"resize":{"flavorRef":2,"OS-DCF:diskConfig":"AUTO"}}`)

	require.Equal(t, http.StatusAccepted, status, "%s", body)
	assert.Empty(t, body)
	resized := detail()
	assert.Equal(t, "VERIFY_RESIZE", resized["status"])
	assert.Equal(t, "resized", resized["OS-EXT-STS:vm_state"])
	assert.Equal(t, float64(1), resized["OS-EXT-STS:power_state"])
	assert.Nil(t, resized["OS-EXT-STS:task_state"])
	assert.Equal(t, map[string]any{
		"id": "2",
		"links": []any{map[string]any{
			"rel": "bookmark", "href": instance.URL + "/compute/flavors/2",
		}},
	}, resized["flavor"])
	requestID := headers.Get("X-Openstack-Request-Id")
	status, _, body = request(t, instance, http.MethodGet,
		computePath+"/servers/"+serverID+"/os-instance-actions/"+requestID, "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	assert.Contains(t, string(body), `"action":"resize"`)

	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L4304-L4310
	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L3454-L3460
	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/vm_states.py#L69-L73
	for _, tt := range []struct {
		body    string
		message string
	}{
		{
			body:    `{"resize":{"flavorRef":"1"}}`,
			message: "Cannot 'resize' instance " + serverID + " while it is in vm_state resized",
		},
		{
			body:    `{"createImage":{"name":"snap"}}`,
			message: "Cannot 'createImage' instance " + serverID + " while it is in vm_state resized",
		},
		{body: `{"reboot":{"type":"SOFT"}}`},
		{body: `{"os-stop":null}`},
	} {
		status, headers, body := action(tt.body)
		requireNovaFault(t, http.StatusConflict, "conflictingRequest", status, headers, body)
		if tt.message != "" {
			assert.Equal(t, tt.message, faultMessage("conflictingRequest", body))
		}
	}

	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/servers.py#L1105-L1127
	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/schemas/servers.py#L516-L524
	status, headers, body = action(`{"revertResize":{"ignored":true}}`)

	require.Equal(t, http.StatusAccepted, status, "%s", body)
	assert.Empty(t, body)
	reverted := detail()
	assert.Equal(t, "ACTIVE", reverted["status"])
	assert.Equal(t, "1", reverted["flavor"].(map[string]any)["id"])

	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/servers.py#L1080-L1084
	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/schemas/servers.py#L507-L514
	require.Equal(t, http.StatusAccepted, responseStatus(action(`{"resize":{"flavorRef":"2"}}`)))

	status, headers, body = action(`{"confirmResize":{}}`)

	require.Equal(t, http.StatusNoContent, status, "%s", body)
	assert.Empty(t, body)
	assert.Empty(t, headers.Get("Content-Type"))
	confirmed := detail()
	assert.Equal(t, "ACTIVE", confirmed["status"])
	assert.Equal(t, "2", confirmed["flavor"].(map[string]any)["id"])

	status, headers, body = request(t, instance, http.MethodPost,
		computePath+"/servers/00000000-0000-4000-8000-000000000000/action", `{"confirmResize":null}`, auth)
	requireNovaFault(t, http.StatusNotFound, "itemNotFound", status, headers, body)
}

func responseStatus(status int, _ http.Header, _ []byte) int {
	return status
}
