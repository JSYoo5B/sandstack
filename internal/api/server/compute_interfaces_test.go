package server_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/attachinterfaces"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/instanceactions"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/servers"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/ports"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func listInterfaces(t *testing.T, client *gophercloud.ServiceClient, serverID string) []attachinterfaces.Interface {
	t.Helper()
	pages, err := attachinterfaces.List(client, serverID).AllPages(t.Context())
	require.NoError(t, err)
	found, err := attachinterfaces.ExtractInterfaces(pages)
	require.NoError(t, err)
	return found
}

func serverActionNames(t *testing.T, client *gophercloud.ServiceClient, serverID string) []string {
	t.Helper()
	pages, err := instanceactions.List(client, serverID, nil).AllPages(t.Context())
	require.NoError(t, err)
	actions, err := instanceactions.ExtractInstanceActions(pages)
	require.NoError(t, err)
	names := make([]string, len(actions))
	for i, action := range actions {
		names[i] = action.Action
	}
	return names
}

// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/attach_interfaces.py#L67-L243
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/network/neutron.py#L1878-L1918
// https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/compute/v2/attachinterfaces/requests.go
func TestGophercloudAttachesAndDetachesServerInterfaces(t *testing.T) {
	instance := startServer(t, "")
	clients := newBootClients(t, instance)
	ctx := t.Context()
	prepareBootNetwork(t, clients.network)
	serverID := bootImageServer(t, clients.compute, "web", fixtureImageID)
	bootPort := serverPorts(t, clients.network, serverID)[0]
	existing, err := ports.Create(ctx, clients.network, ports.CreateOpts{
		NetworkID: fixtureNetworkID, Name: "kept",
	}).Extract()
	require.NoError(t, err)

	created, err := attachinterfaces.Create(ctx, clients.compute, serverID, attachinterfaces.CreateOpts{
		NetworkID: fixtureNetworkID,
		FixedIPs:  []attachinterfaces.FixedIP{{IPAddress: "10.20.0.50"}},
	}).Extract()
	require.NoError(t, err)
	attached, err := attachinterfaces.Create(ctx, clients.compute, serverID, attachinterfaces.CreateOpts{
		PortID: existing.ID,
	}).Extract()

	require.NoError(t, err)
	assert.Equal(t, existing.ID, attached.PortID)
	assert.Equal(t, fixtureNetworkID, attached.NetID)
	assert.Equal(t, existing.MACAddress, attached.MACAddr)
	assert.Equal(t, "DOWN", attached.PortState)
	require.Len(t, existing.FixedIPs, 1)
	assert.Equal(t, []attachinterfaces.FixedIP{{
		SubnetID: existing.FixedIPs[0].SubnetID, IPAddress: existing.FixedIPs[0].IPAddress,
	}}, attached.FixedIPs)
	createdPort, err := ports.Get(ctx, clients.network, created.PortID).Extract()
	require.NoError(t, err)
	assert.Equal(t, serverID, createdPort.DeviceID)
	assert.Equal(t, "compute:nova", createdPort.DeviceOwner)
	assert.Equal(t, []attachinterfaces.FixedIP{{
		SubnetID: createdPort.FixedIPs[0].SubnetID, IPAddress: "10.20.0.50",
	}}, created.FixedIPs)
	listed := listInterfaces(t, clients.compute, serverID)
	listedIDs := make([]string, len(listed))
	for i, item := range listed {
		listedIDs[i] = item.PortID
	}
	assert.ElementsMatch(t, []string{bootPort.ID, created.PortID, existing.ID}, listedIDs)
	shown, err := attachinterfaces.Get(ctx, clients.compute, serverID, existing.ID).Extract()
	require.NoError(t, err)
	assert.Equal(t, attached, shown)
	server, err := servers.Get(ctx, clients.compute, serverID).Extract()
	require.NoError(t, err)
	addresses, ok := server.Addresses["fixture-network"].([]any)
	require.True(t, ok)
	assert.Len(t, addresses, 3)

	require.NoError(t, attachinterfaces.Delete(ctx, clients.compute, serverID, created.PortID).ExtractErr())
	require.NoError(t, attachinterfaces.Delete(ctx, clients.compute, serverID, existing.ID).ExtractErr())

	_, err = ports.Get(ctx, clients.network, created.PortID).Extract()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusNotFound))
	kept, err := ports.Get(ctx, clients.network, existing.ID).Extract()
	require.NoError(t, err)
	assert.Empty(t, kept.DeviceID)
	assert.Empty(t, kept.DeviceOwner)
	listed = listInterfaces(t, clients.compute, serverID)
	require.Len(t, listed, 1)
	assert.Equal(t, bootPort.ID, listed[0].PortID)
	_, err = attachinterfaces.Get(ctx, clients.compute, serverID, existing.ID).Extract()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusNotFound))
	assert.Equal(t, []string{
		"detach_interface", "detach_interface", "attach_interface", "attach_interface", "create",
	}, serverActionNames(t, clients.compute, serverID))
}

func TestNovaServerInterfaceWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	clients := newBootClients(t, instance)
	ctx := t.Context()
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	prepareBootNetwork(t, clients.network)
	serverID := bootImageServer(t, clients.compute, "web", fixtureImageID)
	bootPort := serverPorts(t, clients.network, serverID)[0]
	free, err := ports.Create(ctx, clients.network, ports.CreateOpts{NetworkID: fixtureNetworkID}).Extract()
	require.NoError(t, err)
	missing := "00000000-0000-4000-8000-000000000000"
	interfaces := computePath + "/servers/" + serverID + "/os-interface"
	attach := func(id, body string) (int, http.Header, []byte) {
		t.Helper()
		return request(t, instance, http.MethodPost, computePath+"/servers/"+id+"/os-interface", body, auth)
	}
	faultMessage := func(kind string, body []byte) string {
		t.Helper()
		var faults map[string]struct {
			Message string `json:"message"`
		}
		require.NoError(t, json.Unmarshal(body, &faults))
		return faults[kind].Message
	}

	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/schemas/attach_interfaces.py#L70-L122
	status, headers, body := request(t, instance, http.MethodGet, interfaces, "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	var listed map[string][]map[string]any
	require.NoError(t, json.Unmarshal(body, &listed))
	require.Len(t, listed["interfaceAttachments"], 1)
	assert.Equal(t, map[string]any{
		"port_state": "DOWN",
		"fixed_ips": []any{map[string]any{
			"subnet_id": bootPort.FixedIPs[0].SubnetID, "ip_address": bootPort.FixedIPs[0].IPAddress,
		}},
		"port_id":  bootPort.ID,
		"net_id":   fixtureNetworkID,
		"mac_addr": bootPort.MACAddress,
	}, listed["interfaceAttachments"][0])
	status, headers, body = request(t, instance, http.MethodGet, computePath+"/servers/"+missing+"/os-interface", "", auth)
	requireNovaFault(t, http.StatusNotFound, "itemNotFound", status, headers, body)

	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/attach_interfaces.py#L113-L142
	status, headers, body = request(t, instance, http.MethodGet, interfaces+"/"+free.ID, "", auth)
	requireNovaFault(t, http.StatusNotFound, "itemNotFound", status, headers, body)
	assert.Equal(t, "Instance "+serverID+" does not have a port with id "+free.ID, faultMessage("itemNotFound", body))
	status, headers, body = request(t, instance, http.MethodGet, interfaces+"/"+missing, "", auth)
	requireNovaFault(t, http.StatusNotFound, "itemNotFound", status, headers, body)
	assert.Equal(t, "Port id "+missing+" could not be found.", faultMessage("itemNotFound", body))

	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/schemas/attach_interfaces.py#L19-L50
	for _, invalid := range []string{
		`[]`,
		`{"interfaceAttachment":null}`,
		`{"interfaceAttachment":{},"extra":true}`,
		`{"interfaceAttachment":{"tag":"nic"}}`,
		`{"interfaceAttachment":{"net_id":"not-a-uuid"}}`,
		`{"interfaceAttachment":{"port_id":null}}`,
		`{"interfaceAttachment":{"net_id":"` + fixtureNetworkID + `","fixed_ips":[]}}`,
		`{"interfaceAttachment":{"net_id":"` + fixtureNetworkID +
			`","fixed_ips":[{"ip_address":"10.20.0.9","subnet_id":"x"}]}}`,
		`{"interfaceAttachment":{"net_id":"` + fixtureNetworkID + `","fixed_ips":[{"ip_address":"10.20.0"}]}}`,
	} {
		status, headers, body = attach(missing, invalid)
		requireNovaFault(t, http.StatusBadRequest, "badRequest", status, headers, body)
	}
	status, headers, body = attach(missing, `{"interfaceAttachment":{"port_id":"`+free.ID+`"}}`)
	requireNovaFault(t, http.StatusNotFound, "itemNotFound", status, headers, body)

	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/attach_interfaces.py#L144-L214
	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/network/neutron.py#L472-L561
	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/network/neutron.py#L588-L598
	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/network/neutron.py#L713-L789
	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/network/neutron.py#L914-L998
	for _, rejected := range []struct {
		body    string
		status  int
		kind    string
		message string
	}{
		{`{"interfaceAttachment":{"net_id":"` + fixtureNetworkID + `","port_id":"` + free.ID + `"}}`,
			http.StatusBadRequest, "badRequest", "Must not input both network_id and port_id"},
		{`{"interfaceAttachment":{"fixed_ips":[{"ip_address":"10.20.0.9"}]}}`,
			http.StatusBadRequest, "badRequest", "Must input network_id when request IP address"},
		{`{"interfaceAttachment":{"port_id":"` + missing + `"}}`,
			http.StatusNotFound, "itemNotFound", "Port id " + missing + " could not be found."},
		{`{"interfaceAttachment":{"port_id":"` + bootPort.ID + `"}}`,
			http.StatusConflict, "conflictingRequest", "Port " + bootPort.ID + " is still in use."},
		{`{"interfaceAttachment":{"net_id":"` + missing + `"}}`,
			http.StatusNotFound, "itemNotFound", "Network " + missing + " could not be found."},
		{`{"interfaceAttachment":{"net_id":"` + externalNetworkID + `"}}`,
			http.StatusForbidden, "forbidden",
			"It is not allowed to create an interface on external network " + externalNetworkID},
		{`{"interfaceAttachment":{"net_id":"` + fixtureNetworkID + `","fixed_ips":[{"ip_address":"` +
			bootPort.FixedIPs[0].IPAddress + `"}]}}`,
			http.StatusConflict, "conflictingRequest", "Fixed IP " + bootPort.FixedIPs[0].IPAddress + " is already in use."},
	} {
		status, headers, body = attach(serverID, rejected.body)
		requireNovaFault(t, rejected.status, rejected.kind, status, headers, body)
		assert.Equal(t, rejected.message, faultMessage(rejected.kind, body))
	}
	assert.Equal(t, []string{
		"attach_interface", "attach_interface", "attach_interface", "attach_interface", "attach_interface", "create",
	}, serverActionNames(t, clients.compute, serverID))
	status, headers, body = attach(serverID,
		`{"interfaceAttachment":{"net_id":"`+fixtureNetworkID+`","fixed_ips":[{"ip_address":"192.0.2.10"}]}}`)
	requireNovaFault(t, http.StatusInternalServerError, "computeFault", status, headers, body)

	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/network/neutron.py#L976-L996
	status, headers, body = attach(serverID, `{}`)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	var created map[string]map[string]any
	require.NoError(t, json.Unmarshal(body, &created))
	assert.ElementsMatch(t, []string{"port_state", "fixed_ips", "port_id", "net_id", "mac_addr"},
		mapKeys(created["interfaceAttachment"]))
	assert.Equal(t, fixtureNetworkID, created["interfaceAttachment"]["net_id"])
	createdID, ok := created["interfaceAttachment"]["port_id"].(string)
	require.True(t, ok)

	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/attach_interfaces.py#L216-L243
	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/rpcapi.py#L706-L711
	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/manager.py#L8728-L8744
	status, _, body = request(t, instance, http.MethodDelete, interfaces+"/"+free.ID, "", auth)
	require.Equal(t, http.StatusAccepted, status, "%s", body)
	assert.Empty(t, body)
	status, _, body = request(t, instance, http.MethodDelete, interfaces+"/"+createdID, "", auth)
	require.Equal(t, http.StatusAccepted, status, "%s", body)
	assert.Empty(t, body)
	_, err = ports.Get(ctx, clients.network, createdID).Extract()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusNotFound))
	status, headers, body = request(t, instance, http.MethodDelete,
		computePath+"/servers/"+missing+"/os-interface/"+free.ID, "", auth)
	requireNovaFault(t, http.StatusNotFound, "itemNotFound", status, headers, body)

	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L5492-L5552
	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/common.py#L330-L346
	require.NoError(t, servers.Suspend(ctx, clients.compute, serverID).ExtractErr())
	status, headers, body = attach(serverID, `{"interfaceAttachment":{"port_id":"`+free.ID+`"}}`)
	requireNovaFault(t, http.StatusConflict, "conflictingRequest", status, headers, body)
	assert.Equal(t, "Cannot 'attach_interface' instance "+serverID+" while it is in vm_state suspended",
		faultMessage("conflictingRequest", body))
	status, headers, body = request(t, instance, http.MethodDelete, interfaces+"/"+bootPort.ID, "", auth)
	requireNovaFault(t, http.StatusConflict, "conflictingRequest", status, headers, body)
	assert.Equal(t, "Cannot 'detach_interface' instance "+serverID+" while it is in vm_state suspended",
		faultMessage("conflictingRequest", body))
	require.NoError(t, servers.Resume(ctx, clients.compute, serverID).ExtractErr())
	require.NoError(t, servers.Lock(ctx, clients.compute, serverID).ExtractErr())
	status, headers, body = attach(serverID, `{"interfaceAttachment":{"port_id":"`+free.ID+`"}}`)
	requireNovaFault(t, http.StatusConflict, "conflictingRequest", status, headers, body)
	assert.Equal(t, "Instance "+serverID+" is locked", faultMessage("conflictingRequest", body))
	status, headers, body = request(t, instance, http.MethodDelete, interfaces+"/"+bootPort.ID, "", auth)
	requireNovaFault(t, http.StatusConflict, "conflictingRequest", status, headers, body)
	assert.Equal(t, "Instance "+serverID+" is locked", faultMessage("conflictingRequest", body))
	assert.Equal(t, []string{"lock", "resume", "suspend", "detach_interface", "detach_interface", "attach_interface"},
		serverActionNames(t, clients.compute, serverID)[:6])
}

func mapKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
}
