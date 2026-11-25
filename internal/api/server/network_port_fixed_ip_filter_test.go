package server_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/layer3/routers"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/ports"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/subnets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGophercloudFiltersPortsAfterAddressReplacementAndRelease(t *testing.T) {
	instance := startServer(t, "")
	resources := createRouterInterfaceResources(t, authenticate(t, instance))
	ctx := t.Context()
	service := resources.service
	unaddressed, err := ports.Create(ctx, service, ports.CreateOpts{
		NetworkID: resources.networkID, FixedIPs: []ports.IP{},
	}).Extract()
	require.NoError(t, err)
	const firstIP = "10.66.0.20"
	const nextIP = "10.66.0.21"
	created, err := ports.Create(ctx, service, ports.CreateOpts{
		NetworkID: resources.networkID, FixedIPs: []ports.IP{{IPAddress: firstIP}},
	}).Extract()
	require.NoError(t, err)
	require.NotEmpty(t, created.ID)
	list := func(filters ...ports.FixedIPOpts) []ports.Port {
		t.Helper()
		pages, err := ports.List(service, ports.ListOpts{
			NetworkID: resources.networkID, FixedIPs: filters,
		}).AllPages(ctx)
		require.NoError(t, err)
		result, err := ports.ExtractPorts(pages)
		require.NoError(t, err)
		return result
	}
	byIP := ports.FixedIPOpts{IPAddress: firstIP}
	bySubnet := ports.FixedIPOpts{SubnetID: resources.subnetID}
	both := ports.FixedIPOpts{IPAddress: firstIP, SubnetID: resources.subnetID}
	assert.Equal(t, []ports.Port{*created}, list(byIP))
	assert.Equal(t, []ports.Port{*created}, list(bySubnet))
	assert.Equal(t, []ports.Port{*created}, list(both))

	replaced, err := ports.Update(ctx, service, created.ID, ports.UpdateOpts{
		FixedIPs: []ports.IP{{SubnetID: resources.subnetID, IPAddress: nextIP}},
	}).Extract()

	require.NoError(t, err)
	assert.Empty(t, list(byIP))
	assert.Empty(t, list(both))
	assert.Equal(t, []ports.Port{*replaced}, list(bySubnet))
	both.IPAddress = nextIP
	assert.Equal(t, []ports.Port{*replaced}, list(both))
	assert.Equal(t, []ports.Port{*replaced}, list(byIP, ports.FixedIPOpts{IPAddress: nextIP}, bySubnet))

	released, err := ports.Update(ctx, service, created.ID, ports.UpdateOpts{FixedIPs: []ports.IP{}}).Extract()

	require.NoError(t, err)
	assert.Empty(t, list(both))
	assert.Empty(t, list(bySubnet))
	assert.ElementsMatch(t, []ports.Port{*released, *unaddressed}, list())
}

func TestPortFixedIPFiltersWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	resources := createRouterInterfaceResources(t, provider)
	ctx := t.Context()
	service := resources.service
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	createSubnet := func(networkID, cidr string) string {
		t.Helper()
		subnet, err := subnets.Create(ctx, service, subnets.CreateOpts{
			NetworkID: networkID, CIDR: cidr, IPVersion: gophercloud.IPv4,
		}).Extract()
		require.NoError(t, err)
		require.NotEmpty(t, subnet.ID)
		return subnet.ID
	}
	createPort := func(networkID string, fixedIPs []ports.IP) string {
		t.Helper()
		port, err := ports.Create(ctx, service, ports.CreateOpts{
			NetworkID: networkID, FixedIPs: fixedIPs,
		}).Extract()
		require.NoError(t, err)
		require.NotEmpty(t, port.ID)
		return port.ID
	}
	secondSubnet := createSubnet(resources.networkID, "10.67.0.0/24")
	otherNetworkSubnet := createSubnet(fixtureNetworkID, "10.66.0.0/24")
	const firstIP = "10.66.0.2"
	const secondIP = "10.67.0.2"
	first := createPort(resources.networkID, []ports.IP{{SubnetID: resources.subnetID, IPAddress: firstIP}})
	second := createPort(resources.networkID, []ports.IP{{SubnetID: secondSubnet, IPAddress: secondIP}})
	otherNetwork := createPort(fixtureNetworkID, []ports.IP{{SubnetID: otherNetworkSubnet, IPAddress: firstIP}})
	createPort(resources.networkID, []ports.IP{})
	bound, err := routers.AddInterface(ctx, service, resources.routerID, routers.AddInterfaceOpts{
		SubnetID: resources.subnetID,
	}).Extract()
	require.NoError(t, err)
	require.NotEmpty(t, bound.PortID)
	const unknownSubnet = "00000000-0000-0000-0000-000000000000"

	// https://github.com/openstack/neutron/blob/master/neutron/db/db_base_plugin_v2.py#L1551-L1582
	for _, test := range []struct {
		name  string
		query url.Values
		ids   []string
	}{
		{
			name:  "repeated exact addresses",
			query: url.Values{"fixed_ips": {"ip_address=" + firstIP, "ip_address=" + secondIP, "ip_address=" + firstIP}},
			ids:   []string{first, second, otherNetwork},
		},
		{
			name:  "repeated subnets exclude addressless ports",
			query: url.Values{"fixed_ips": {"subnet_id=" + resources.subnetID, "subnet_id=" + secondSubnet}},
			ids:   []string{first, second, bound.PortID},
		},
		{
			name:  "address and subnet independently constrain",
			query: url.Values{"fixed_ips": {"ip_address=" + firstIP, "subnet_id=" + secondSubnet}},
		},
		{
			name:  "network narrows matching addresses",
			query: url.Values{"fixed_ips": {"ip_address=" + firstIP}, "network_id": {resources.networkID}},
			ids:   []string{first},
		},
		{
			name: "fixed IP combines with network and device filters",
			query: url.Values{
				"fixed_ips":  {"ip_address=" + firstIP, "ip_address=10.66.0.1", "subnet_id=" + resources.subnetID},
				"network_id": {resources.networkID}, "device_id": {resources.routerID},
				"device_owner": {routerInterfaceOwner},
			},
			ids: []string{bound.PortID},
		},
		{name: "literal address fragment is not a substring match", query: url.Values{"fixed_ips": {"ip_address=10.66.0"}}},
		{name: "unknown subnet", query: url.Values{"fixed_ips": {"subnet_id=" + unknownSubnet}}},
		{name: "empty address", query: url.Values{"fixed_ips": {"ip_address="}}},
		{name: "empty subnet", query: url.Values{"fixed_ips": {"subnet_id="}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			status, headers, body := request(t, instance, http.MethodGet, portsPath+"?"+test.query.Encode(), "", auth)
			require.Equal(t, http.StatusOK, status, "%s", body)
			requireJSON(t, headers)
			var response struct {
				Ports []wirePort `json:"ports"`
			}
			require.NoError(t, json.Unmarshal(body, &response))
			require.NotNil(t, response.Ports)
			var ids []string
			for _, port := range response.Ports {
				ids = append(ids, port.ID)
			}
			assert.ElementsMatch(t, test.ids, ids)
		})
	}

	for _, invalid := range []string{"", "ip_address", "ip_address_substr=10.66", "=10.66.0.2", "unknown=10.66.0.2"} {
		query := url.Values{"fixed_ips": {"ip_address=" + firstIP, invalid}}
		status, headers, body := request(t, instance, http.MethodGet, portsPath+"?"+query.Encode(), "", auth)
		require.Equal(t, http.StatusBadRequest, status, "%q: %s", invalid, body)
		requireJSON(t, headers)
		assertPortNeutronError(t, body, "BadRequest")
	}
}
