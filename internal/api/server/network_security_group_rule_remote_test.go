package server_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/security/groups"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/security/rules"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGophercloudCreatesNeutronRemoteGroupAndICMPRules(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	client, err := openstack.NewNetworkV2(provider, gophercloud.EndpointOpts{
		Region: "RegionOne", Availability: gophercloud.AvailabilityPublic,
	})
	require.NoError(t, err)
	ctx := t.Context()
	web, err := groups.Create(ctx, client, groups.CreateOpts{Name: "web"}).Extract()
	require.NoError(t, err)
	database, err := groups.Create(ctx, client, groups.CreateOpts{Name: "database"}).Extract()
	require.NoError(t, err)

	// https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/db/securitygroups_db.py#L856-L913
	fromWeb, err := rules.Create(ctx, client, rules.CreateOpts{
		SecGroupID: database.ID, Direction: rules.DirIngress, EtherType: rules.EtherType4,
		Protocol: rules.ProtocolTCP, PortRangeMin: 5432, PortRangeMax: 5432, RemoteGroupID: web.ID,
	}).Extract()
	require.NoError(t, err)
	anyFromWeb, err := rules.Create(ctx, client, rules.CreateOpts{
		SecGroupID: database.ID, Direction: rules.DirIngress, EtherType: rules.EtherType6, RemoteGroupID: web.ID,
	}).Extract()
	require.NoError(t, err)
	ping, err := rules.Create(ctx, client, rules.CreateOpts{
		SecGroupID: database.ID, Direction: rules.DirIngress, EtherType: rules.EtherType4,
		Protocol: rules.ProtocolICMP, PortRangeMin: 8, RemoteIPPrefix: "192.0.2.0/24",
	}).Extract()
	require.NoError(t, err)
	ping6, err := rules.Create(ctx, client, rules.CreateOpts{
		SecGroupID: database.ID, Direction: rules.DirIngress, EtherType: rules.EtherType6,
		Protocol: rules.ProtocolICMP, PortRangeMin: 128,
	}).Extract()
	require.NoError(t, err)

	assert.Equal(t, web.ID, fromWeb.RemoteGroupID)
	assert.Empty(t, fromWeb.RemoteIPPrefix)
	assert.Equal(t, "tcp", fromWeb.Protocol)
	assert.Equal(t, 5432, fromWeb.PortRangeMin)
	assert.Equal(t, web.ID, anyFromWeb.RemoteGroupID)
	assert.Empty(t, anyFromWeb.Protocol)
	assert.Equal(t, "icmp", ping.Protocol)
	assert.Equal(t, 8, ping.PortRangeMin)
	assert.Zero(t, ping.PortRangeMax)
	assert.Equal(t, "ipv6-icmp", ping6.Protocol)
	assert.Equal(t, 128, ping6.PortRangeMin)
	for _, created := range []*rules.SecGroupRule{fromWeb, anyFromWeb, ping, ping6} {
		read, err := rules.Get(ctx, client, created.ID).Extract()
		require.NoError(t, err)
		assert.Equal(t, created, read)
	}
	list := func() []rules.SecGroupRule {
		t.Helper()
		pages, err := rules.List(client, rules.ListOpts{SecGroupID: database.ID, Direction: "ingress"}).AllPages(ctx)
		require.NoError(t, err)
		found, err := rules.ExtractRules(pages)
		require.NoError(t, err)
		return found
	}
	assert.ElementsMatch(t, []rules.SecGroupRule{*fromWeb, *anyFromWeb, *ping, *ping6}, list())

	_, err = rules.Create(ctx, client, rules.CreateOpts{
		SecGroupID: database.ID, Direction: rules.DirIngress, EtherType: rules.EtherType6,
		Protocol: "icmpv6", PortRangeMin: 128,
	}).Extract()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusConflict), "%v", err)
	_, err = rules.Create(ctx, client, rules.CreateOpts{
		SecGroupID: database.ID, Direction: rules.DirIngress, EtherType: rules.EtherType4,
		Protocol: rules.ProtocolTCP, RemoteGroupID: "00000000-0000-0000-0000-000000000000",
	}).Extract()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusNotFound), "%v", err)

	// https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/db/models/securitygroup.py#L73-L101
	require.NoError(t, groups.Delete(ctx, client, web.ID).ExtractErr())
	_, err = rules.Get(ctx, client, fromWeb.ID).Extract()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusNotFound), "%v", err)
	assert.ElementsMatch(t, []rules.SecGroupRule{*ping, *ping6}, list())
}

func TestNeutronSecurityGroupRuleRemoteAndICMPWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	client, err := openstack.NewNetworkV2(provider, gophercloud.EndpointOpts{
		Region: "RegionOne", Availability: gophercloud.AvailabilityPublic,
	})
	require.NoError(t, err)
	ctx := t.Context()
	web, err := groups.Create(ctx, client, groups.CreateOpts{Name: "web"}).Extract()
	require.NoError(t, err)
	database, err := groups.Create(ctx, client, groups.CreateOpts{Name: "database"}).Extract()
	require.NoError(t, err)
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	create := func(t *testing.T, fields map[string]any, expected int) []byte {
		t.Helper()
		payload := map[string]any{"security_group_id": database.ID, "direction": "ingress", "ethertype": "IPv4"}
		for field, value := range fields {
			payload[field] = value
		}
		encoded, err := json.Marshal(map[string]any{"security_group_rule": payload})
		require.NoError(t, err)
		status, headers, body := request(t, instance, http.MethodPost, securityGroupRulesPath, string(encoded), auth)
		require.Equal(t, expected, status, "%s", body)
		requireJSON(t, headers)
		return body
	}
	createRule := func(t *testing.T, fields map[string]any) map[string]json.RawMessage {
		t.Helper()
		var response struct {
			Rule map[string]json.RawMessage `json:"security_group_rule"`
		}
		require.NoError(t, json.Unmarshal(create(t, fields, http.StatusCreated), &response))
		return response.Rule
	}

	// https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/extensions/securitygroup.py#L151-L185
	// https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/db/securitygroups_db.py#L770-L838
	// https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/db/securitygroups_db.py#L522-L527
	for _, tc := range []struct {
		name      string
		fields    map[string]any
		errorType string
	}{
		{"remote group and prefix", map[string]any{
			"remote_group_id": web.ID, "remote_ip_prefix": "192.0.2.0/24",
		}, "SecurityGroupMultipleRemoteEntites"},
		{"ICMP code without type", map[string]any{"protocol": "icmp", "port_range_max": 0}, "SecurityGroupMissingIcmpType"},
		{"ICMP type above 255", map[string]any{"protocol": "icmp", "port_range_min": 256}, "SecurityGroupInvalidIcmpValue"},
		{"negative ICMP type", map[string]any{"protocol": 1, "port_range_min": -1}, "SecurityGroupInvalidPortValue"},
		{"ports without protocol", map[string]any{
			"protocol": nil, "port_range_min": 22, "port_range_max": 22,
		}, "SecurityGroupProtocolRequiredWithPorts"},
		{"IPv6 ICMP name on IPv4", map[string]any{"protocol": "icmpv6"}, "SecurityGroupEthertypeConflictWithProtocol"},
		{"IPv6 ICMP number on IPv4", map[string]any{"protocol": "58"}, "SecurityGroupEthertypeConflictWithProtocol"},
		{"unknown protocol name", map[string]any{"protocol": "bogus"}, "SecurityGroupRuleInvalidProtocol"},
		{"protocol number above 255", map[string]any{"protocol": 256}, "SecurityGroupRuleInvalidProtocol"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := create(t, tc.fields, http.StatusBadRequest)

			assertPortNeutronError(t, body, tc.errorType)
		})
	}
	missing := create(t, map[string]any{"remote_group_id": "00000000-0000-0000-0000-000000000000"}, http.StatusNotFound)
	assertPortNeutronError(t, missing, "SecurityGroupNotFound")
	afterRejects, err := groups.Get(ctx, client, database.ID).Extract()
	require.NoError(t, err)
	assert.ElementsMatch(t, database.Rules, afterRejects.Rules)

	anyFromWeb := createRule(t, map[string]any{
		"protocol": "any", "remote_group_id": web.ID, "remote_ip_prefix": nil,
		"port_range_min": 1, "port_range_max": 65535,
	})
	echoReply := createRule(t, map[string]any{"protocol": "ICMP", "port_range_min": 0})
	routerAdvert := createRule(t, map[string]any{
		"ethertype": "IPv6", "protocol": "1", "port_range_min": 134, "port_range_max": 0,
	})

	assert.JSONEq(t, "null", string(anyFromWeb["protocol"]))
	assert.JSONEq(t, `"`+web.ID+`"`, string(anyFromWeb["remote_group_id"]))
	assert.JSONEq(t, "null", string(anyFromWeb["remote_ip_prefix"]))
	assert.JSONEq(t, "null", string(anyFromWeb["normalized_cidr"]))
	assert.JSONEq(t, "null", string(anyFromWeb["port_range_min"]))
	assert.JSONEq(t, "null", string(anyFromWeb["port_range_max"]))
	assert.JSONEq(t, `"icmp"`, string(echoReply["protocol"]))
	assert.JSONEq(t, "0", string(echoReply["port_range_min"]))
	assert.JSONEq(t, "null", string(echoReply["port_range_max"]))
	assert.JSONEq(t, "null", string(echoReply["remote_group_id"]))
	assert.JSONEq(t, `"58"`, string(routerAdvert["protocol"]))
	assert.JSONEq(t, "134", string(routerAdvert["port_range_min"]))
	assert.JSONEq(t, "0", string(routerAdvert["port_range_max"]))

	// https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/db/securitygroups_db.py#L965-L1014
	for _, tc := range []struct {
		name   string
		fields map[string]any
	}{
		{"null protocol", map[string]any{"protocol": nil, "remote_group_id": web.ID}},
		{"ICMP number", map[string]any{"protocol": 1, "port_range_min": 0}},
		{"IPv6 ICMP name", map[string]any{
			"ethertype": "IPv6", "protocol": "ipv6-icmp", "port_range_min": 134, "port_range_max": 0,
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := create(t, tc.fields, http.StatusConflict)

			assertPortNeutronError(t, body, "SecurityGroupRuleExists")
		})
	}
}
