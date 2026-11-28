package server_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/secgroups"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/servers"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/portsecurity"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/security/groups"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/security/rules"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/ports"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func listServerSecurityGroups(t *testing.T, client *gophercloud.ServiceClient, serverID string) []secgroups.SecurityGroup {
	t.Helper()
	pages, err := secgroups.ListByServer(client, serverID).AllPages(t.Context())
	require.NoError(t, err)
	found, err := secgroups.ExtractSecurityGroups(pages)
	require.NoError(t, err)
	return found
}

// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/security_groups.py#L364-L468
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/network/security_group_api.py#L587-L709
// https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/compute/v2/secgroups/requests.go#L182-L193
func TestGophercloudAddsAndRemovesServerSecurityGroup(t *testing.T) {
	instance := startServer(t, "")
	clients := newBootClients(t, instance)
	ctx := t.Context()
	prepareBootNetwork(t, clients.network)
	serverID := bootImageServer(t, clients.compute, "web", fixtureImageID)
	web, err := groups.Create(ctx, clients.network, groups.CreateOpts{
		Name: "web", Description: "Web access",
	}).Extract()
	require.NoError(t, err)
	ssh, err := rules.Create(ctx, clients.network, rules.CreateOpts{
		SecGroupID: web.ID, Direction: rules.DirIngress, EtherType: rules.EtherType4,
		Protocol: rules.ProtocolTCP, PortRangeMin: 22, PortRangeMax: 22, RemoteIPPrefix: "192.0.2.0/24",
	}).Extract()
	require.NoError(t, err)

	addErr := secgroups.AddServer(ctx, clients.compute, serverID, "web").ExtractErr()

	require.NoError(t, addErr)
	attached := serverPorts(t, clients.network, serverID)
	require.Len(t, attached, 1)
	assert.Contains(t, attached[0].SecurityGroups, web.ID)
	listed := listServerSecurityGroups(t, clients.compute, serverID)
	require.Len(t, listed, 2)
	assert.Equal(t, "default", listed[0].Name)
	assert.Equal(t, web.ID, listed[1].ID)
	assert.Equal(t, "web", listed[1].Name)
	assert.Equal(t, "Web access", listed[1].Description)
	assert.Equal(t, web.TenantID, listed[1].TenantID)
	require.Len(t, listed[1].Rules, 1)
	assert.Equal(t, ssh.ID, listed[1].Rules[0].ID)
	assert.Equal(t, web.ID, listed[1].Rules[0].ParentGroupID)
	assert.Equal(t, "tcp", listed[1].Rules[0].IPProtocol)
	assert.Equal(t, 22, listed[1].Rules[0].FromPort)
	assert.Equal(t, 22, listed[1].Rules[0].ToPort)
	assert.Equal(t, "192.0.2.0/24", listed[1].Rules[0].IPRange.CIDR)
	server, err := servers.Get(ctx, clients.compute, serverID).Extract()
	require.NoError(t, err)
	assert.Equal(t, []map[string]any{{"name": "default"}, {"name": "web"}}, server.SecurityGroups)

	removeErr := secgroups.RemoveServer(ctx, clients.compute, serverID, web.ID).ExtractErr()

	require.NoError(t, removeErr)
	listed = listServerSecurityGroups(t, clients.compute, serverID)
	require.Len(t, listed, 1)
	assert.Equal(t, "default", listed[0].Name)
	server, err = servers.Get(ctx, clients.compute, serverID).Extract()
	require.NoError(t, err)
	assert.Equal(t, []map[string]any{{"name": "default"}}, server.SecurityGroups)
	err = secgroups.RemoveServer(ctx, clients.compute, serverID, "web").ExtractErr()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusNotFound))
}

func TestNovaServerSecurityGroupWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	clients := newBootClients(t, instance)
	ctx := t.Context()
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
	var defaultID string
	for _, port := range serverPorts(t, clients.network, serverID) {
		require.Len(t, port.SecurityGroups, 1)
		defaultID = port.SecurityGroups[0]
	}
	for _, name := range []string{"twin", "twin"} {
		_, err := groups.Create(ctx, clients.network, groups.CreateOpts{Name: name}).Extract()
		require.NoError(t, err)
	}
	stateful := false
	stateless, err := groups.Create(ctx, clients.network, groups.CreateOpts{
		Name: "stateless", Stateful: &stateful,
	}).Extract()
	require.NoError(t, err)
	missingServer := "00000000-0000-4000-8000-000000000000"

	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/schemas/security_groups.py#L106-L144
	for _, actionName := range []string{"addSecurityGroup", "removeSecurityGroup"} {
		for _, body := range []string{
			`{"` + actionName + `":null}`,
			`{"` + actionName + `":{}}`,
			`{"` + actionName + `":{"name":""}}`,
			`{"` + actionName + `":{"name":null}}`,
			`{"` + actionName + `":{"name":1}}`,
			`{"` + actionName + `":{"name":"web","extra":true}}`,
		} {
			status, headers, response := action(missingServer, body)
			requireNovaFault(t, http.StatusBadRequest, "badRequest", status, headers, response)
		}
		status, headers, body := action(serverID, `{"`+actionName+`":{}}`)
		requireNovaFault(t, http.StatusBadRequest, "badRequest", status, headers, body)
		assert.Equal(t, "'name' is a required property.", faultMessage("badRequest", body))

		// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/security_groups.py#L407-L422
		status, headers, body = action(missingServer, `{"`+actionName+`":{"name":" "}}`)
		requireNovaFault(t, http.StatusNotFound, "itemNotFound", status, headers, body)
		status, headers, body = action(serverID, `{"`+actionName+`":{"name":" "}}`)
		requireNovaFault(t, http.StatusBadRequest, "badRequest", status, headers, body)
		assert.Equal(t, "Security group name cannot be empty", faultMessage("badRequest", body))

		// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/network/security_group_api.py#L590-L606
		// https://github.com/openstack/python-neutronclient/blob/11.6.0/neutronclient/common/exceptions.py#L258-L260
		status, headers, body = action(serverID, `{"`+actionName+`":{"name":"absent"}}`)
		requireNovaFault(t, http.StatusNotFound, "itemNotFound", status, headers, body)
		assert.Equal(t, "Security group absent is not found for project "+stateless.TenantID,
			faultMessage("itemNotFound", body))
		status, headers, body = action(serverID, `{"`+actionName+`":{"name":"twin"}}`)
		requireNovaFault(t, http.StatusConflict, "conflictingRequest", status, headers, body)
		assert.Equal(t, "Multiple security_group matches found for name 'twin', use an ID to be more specific.",
			faultMessage("conflictingRequest", body))

		// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/network/security_group_api.py#L614-L617
		status, headers, body = action(fixtureServerID, `{"`+actionName+`":{"name":"default"}}`)
		requireNovaFault(t, http.StatusNotFound, "itemNotFound", status, headers, body)
		assert.Equal(t, "instance_id "+fixtureServerID+" could not be found as device id on any ports",
			faultMessage("itemNotFound", body))
	}

	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/network/security_group_api.py#L619-L647
	// https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/extensions/securitygroup.py#L288-L295
	// https://github.com/openstack/neutron-lib/blob/5.0.0/neutron_lib/api/attributes.py#L208-L237
	status, headers, body := action(serverID, `{"addSecurityGroup":{"name":"default"}}`)
	requireNovaFault(t, http.StatusBadRequest, "badRequest", status, headers, body)
	assert.Equal(t, "Invalid input for operation: Invalid input for security_groups. "+
		"Reason: Duplicate items in the list: '"+defaultID+"'..", faultMessage("badRequest", body))
	status, headers, body = action(serverID, `{"addSecurityGroup":{"name":"`+stateless.ID+`"}}`)
	requireNovaFault(t, http.StatusConflict, "conflictingRequest", status, headers, body)

	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/network/security_group_api.py#L703-L709
	status, headers, body = action(serverID, `{"removeSecurityGroup":{"name":"stateless"}}`)
	requireNovaFault(t, http.StatusNotFound, "itemNotFound", status, headers, body)
	assert.Equal(t, "Security group stateless not associated with the instance "+serverID,
		faultMessage("itemNotFound", body))

	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/security_groups.py#L424-L468
	require.Equal(t, http.StatusAccepted, responseStatus(action(serverID, `{"lock":null}`)))
	status, headers, body = action(serverID, `{"removeSecurityGroup":{"name":"default"}}`)
	require.Equal(t, http.StatusAccepted, status, "%s", body)
	assert.Empty(t, body)
	assert.Equal(t, "no-store", headers.Get("Cache-Control"))
	status, _, body = action(serverID, `{"addSecurityGroup":{"name":"`+defaultID+`"}}`)
	require.Equal(t, http.StatusAccepted, status, "%s", body)
	assert.Empty(t, body)

	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/network/security_group_api.py#L578-L584
	attached := serverPorts(t, clients.network, serverID)
	require.Len(t, attached, 1)
	disabled, cleared := false, []string{}
	_, err = ports.Update(ctx, clients.network, attached[0].ID, portsecurity.PortUpdateOptsExt{
		UpdateOptsBuilder:   ports.UpdateOpts{SecurityGroups: &cleared},
		PortSecurityEnabled: &disabled,
	}).Extract()
	require.NoError(t, err)
	status, headers, body = action(serverID, `{"addSecurityGroup":{"name":"default"}}`)
	requireNovaFault(t, http.StatusBadRequest, "badRequest", status, headers, body)
	assert.Equal(t, "Network requires port_security_enabled and subnet associated in order to apply security groups.",
		faultMessage("badRequest", body))
}

func TestNovaServerSecurityGroupListWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	clients := newBootClients(t, instance)
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	prepareBootNetwork(t, clients.network)
	serverID := bootImageServer(t, clients.compute, "web", fixtureImageID)

	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/security_groups.py#L46-L103
	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/schemas/security_groups.py#L146-L246
	status, headers, body := request(t, instance, http.MethodGet,
		computePath+"/servers/"+serverID+"/os-security-groups?limit=1", "", auth)

	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	var response struct {
		SecurityGroups []map[string]json.RawMessage `json:"security_groups"`
	}
	require.NoError(t, json.Unmarshal(body, &response))
	require.Len(t, response.SecurityGroups, 1)
	group := response.SecurityGroups[0]
	assert.ElementsMatch(t, []string{"description", "id", "name", "rules", "tenant_id"}, keysOf(group))
	assert.JSONEq(t, `"default"`, string(group["name"]))
	var groupRules []map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(group["rules"], &groupRules))
	require.Len(t, groupRules, 2)
	for _, rule := range groupRules {
		assert.ElementsMatch(t, []string{
			"from_port", "group", "id", "ip_protocol", "ip_range", "parent_group_id", "to_port",
		}, keysOf(rule))
		assert.JSONEq(t, `null`, string(rule["ip_protocol"]))
		assert.JSONEq(t, `null`, string(rule["from_port"]))
		assert.JSONEq(t, `null`, string(rule["to_port"]))
		assert.JSONEq(t, `{}`, string(rule["ip_range"]))
		assert.JSONEq(t, `{"name":"default","tenant_id":`+string(group["tenant_id"])+`}`, string(rule["group"]))
		assert.Equal(t, string(group["id"]), string(rule["parent_group_id"]))
	}

	status, headers, body = request(t, instance, http.MethodGet,
		computePath+"/servers/00000000-0000-4000-8000-000000000000/os-security-groups", "", auth)
	requireNovaFault(t, http.StatusNotFound, "itemNotFound", status, headers, body)
}
