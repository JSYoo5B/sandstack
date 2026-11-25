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

func TestGophercloudCreatesAndDeletesNeutronSecurityGroupRules(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	client, err := openstack.NewNetworkV2(provider, gophercloud.EndpointOpts{
		Region: "RegionOne", Availability: gophercloud.AvailabilityPublic,
	})
	require.NoError(t, err)
	ctx := t.Context()
	group, err := groups.Create(ctx, client, groups.CreateOpts{Name: "application ingress"}).Extract()
	require.NoError(t, err)
	opts := rules.CreateOpts{
		SecGroupID: group.ID, Direction: "ingress", EtherType: "IPv4", Protocol: "tcp",
		Description: "HTTPS", PortRangeMin: 443, PortRangeMax: 443, RemoteIPPrefix: "192.0.2.7/24",
	}

	created, err := rules.Create(ctx, client, opts).Extract()
	require.NoError(t, err)

	require.NotEmpty(t, created.ID)
	assert.Equal(t, group.ID, created.SecGroupID)
	assert.Equal(t, routerProjectID(t, provider), created.ProjectID)
	assert.Equal(t, "ingress", created.Direction)
	assert.Equal(t, "IPv4", created.EtherType)
	assert.Equal(t, "tcp", created.Protocol)
	assert.Equal(t, opts.Description, created.Description)
	assert.Equal(t, 443, created.PortRangeMin)
	assert.Equal(t, 443, created.PortRangeMax)
	assert.Equal(t, "192.0.2.7/24", created.RemoteIPPrefix)
	read, err := rules.Get(ctx, client, created.ID).Extract()
	require.NoError(t, err)
	assert.Equal(t, created, read)

	auth := map[string]string{"X-Auth-Token": provider.Token()}
	path := securityGroupRulesPath + "/" + created.ID
	status, headers, body := request(t, instance, http.MethodGet, path, "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	var wire struct {
		Rule map[string]json.RawMessage `json:"security_group_rule"`
	}
	require.NoError(t, json.Unmarshal(body, &wire))
	assert.JSONEq(t, `"192.0.2.7/24"`, string(wire.Rule["remote_ip_prefix"]))
	assert.JSONEq(t, `"192.0.2.0/24"`, string(wire.Rule["normalized_cidr"]))
	assert.JSONEq(t, "null", string(wire.Rule["remote_group_id"]))
	assert.JSONEq(t, "null", string(wire.Rule["remote_address_group_id"]))

	duplicate := opts
	duplicate.Description = "Different description"
	_, err = rules.Create(ctx, client, duplicate).Extract()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusConflict), "%v", err)

	otherHost := opts
	otherHost.RemoteIPPrefix = "192.0.2.8/24"
	second, err := rules.Create(ctx, client, otherHost).Extract()
	require.NoError(t, err)
	require.NotEmpty(t, second.ID)
	assert.NotEqual(t, created.ID, second.ID)
	assert.Equal(t, otherHost.RemoteIPPrefix, second.RemoteIPPrefix)
	udp6, err := rules.Create(ctx, client, rules.CreateOpts{
		SecGroupID: group.ID, Direction: "ingress", EtherType: "IPv6", Protocol: "udp",
		RemoteIPPrefix: "2001:db8::7/64",
	}).Extract()
	require.NoError(t, err)
	assert.Equal(t, "udp", udp6.Protocol)
	assert.Equal(t, "2001:db8::7/64", udp6.RemoteIPPrefix)
	assert.Zero(t, udp6.PortRangeMin)
	assert.Zero(t, udp6.PortRangeMax)

	list := func() []rules.SecGroupRule {
		t.Helper()
		pages, err := rules.List(client, rules.ListOpts{SecGroupID: group.ID, Direction: "ingress"}).AllPages(ctx)
		require.NoError(t, err)
		found, err := rules.ExtractRules(pages)
		require.NoError(t, err)
		return found
	}
	assert.ElementsMatch(t, []rules.SecGroupRule{*created, *second, *udp6}, list())
	readGroup, err := groups.Get(ctx, client, group.ID).Extract()
	require.NoError(t, err)
	expected := append(append([]rules.SecGroupRule{}, group.Rules...), *created, *second, *udp6)
	assert.ElementsMatch(t, expected, readGroup.Rules)

	require.NoError(t, rules.Delete(ctx, client, created.ID).ExtractErr())
	_, err = rules.Get(ctx, client, created.ID).Extract()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusNotFound), "%v", err)
	assert.ElementsMatch(t, []rules.SecGroupRule{*second, *udp6}, list())
	readGroup, err = groups.Get(ctx, client, group.ID).Extract()
	require.NoError(t, err)
	expected = append(append([]rules.SecGroupRule{}, group.Rules...), *second, *udp6)
	assert.ElementsMatch(t, expected, readGroup.Rules)
}

func TestNeutronSecurityGroupRuleLifecycleWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	client, err := openstack.NewNetworkV2(provider, gophercloud.EndpointOpts{
		Region: "RegionOne", Availability: gophercloud.AvailabilityPublic,
	})
	require.NoError(t, err)
	ctx := t.Context()
	group, err := groups.Create(ctx, client, groups.CreateOpts{Name: "wire rules"}).Extract()
	require.NoError(t, err)
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	valid := map[string]any{
		"security_group_id": group.ID, "direction": "ingress", "ethertype": "IPv4", "protocol": "tcp",
		"remote_ip_prefix": "192.0.2.7", "port_range_min": 1, "port_range_max": 65535,
	}
	create := func(t *testing.T, overrides map[string]any, expected int) []byte {
		t.Helper()
		payload := make(map[string]any, len(valid))
		for field, value := range valid {
			payload[field] = value
		}
		for field, value := range overrides {
			payload[field] = value
		}
		encoded, err := json.Marshal(map[string]any{"security_group_rule": payload})
		require.NoError(t, err)
		status, headers, body := request(t, instance, http.MethodPost, securityGroupRulesPath, string(encoded), auth)
		require.Equal(t, expected, status, "%s", body)
		requireJSON(t, headers)
		return body
	}

	// https://docs.openstack.org/api-ref/network/v2/#create-security-group-rule
	// https://docs.openstack.org/api-ref/network/v2/#delete-security-group-rule
	for _, tc := range []struct {
		name      string
		overrides map[string]any
	}{
		{"reversed ports", map[string]any{"port_range_min": 444, "port_range_max": 443}},
		{"port zero", map[string]any{"port_range_min": 0}},
		{"wrong address family", map[string]any{"remote_ip_prefix": "2001:db8::/64"}},
		{"unsupported protocol", map[string]any{"protocol": "sctp"}},
		{"unknown direction", map[string]any{"direction": "sideways"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			create(t, tc.overrides, http.StatusBadRequest)
		})
	}
	create(t, map[string]any{"security_group_id": "00000000-0000-0000-0000-000000000000"}, http.StatusNotFound)
	afterRejects, err := groups.Get(ctx, client, group.ID).Extract()
	require.NoError(t, err)
	assert.ElementsMatch(t, group.Rules, afterRejects.Rules)

	body := create(t, nil, http.StatusCreated)
	var created struct {
		Rule map[string]json.RawMessage `json:"security_group_rule"`
	}
	require.NoError(t, json.Unmarshal(body, &created))
	var id string
	require.NoError(t, json.Unmarshal(created.Rule["id"], &id))
	require.NotEmpty(t, id)
	assert.JSONEq(t, `"192.0.2.7/32"`, string(created.Rule["remote_ip_prefix"]))
	assert.JSONEq(t, `"192.0.2.7/32"`, string(created.Rule["normalized_cidr"]))
	assert.JSONEq(t, "null", string(created.Rule["port_range_min"]))
	assert.JSONEq(t, "null", string(created.Rule["port_range_max"]))

	path := securityGroupRulesPath + "/" + id
	status, _, body := request(t, instance, http.MethodDelete, path, "", nil)
	assert.Equal(t, http.StatusUnauthorized, status, "%s", body)
	status, _, body = request(t, instance, http.MethodDelete, path, "", auth)
	assert.Equal(t, http.StatusNoContent, status, "%s", body)
	assert.Empty(t, body)
	status, _, body = request(t, instance, http.MethodDelete, path, "", auth)
	assert.Equal(t, http.StatusNotFound, status, "%s", body)
}
