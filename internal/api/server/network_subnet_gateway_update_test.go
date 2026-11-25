package server_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/layer3/routers"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/ports"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/subnets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGophercloudUpdatesSubnetGatewayWithoutChangingPools(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	client, err := openstack.NewNetworkV2(provider, gophercloud.EndpointOpts{
		Region: "RegionOne", Availability: gophercloud.AvailabilityPublic,
	})
	require.NoError(t, err)
	ctx := t.Context()
	pool := []subnets.AllocationPool{{Start: "10.83.0.10", End: "10.83.0.20"}}
	baseline, err := subnets.Create(ctx, client, subnets.CreateOpts{
		Name: "gateway-update", NetworkID: fixtureNetworkID, CIDR: "10.83.0.0/24",
		IPVersion: gophercloud.IPv4, AllocationPools: pool,
	}).Extract()
	require.NoError(t, err)
	require.Equal(t, "10.83.0.1", baseline.GatewayIP)
	require.Equal(t, pool, baseline.AllocationPools)
	read := func() *subnets.Subnet {
		t.Helper()
		subnet, err := subnets.Get(ctx, client, baseline.ID).Extract()
		require.NoError(t, err)
		return subnet
	}
	update := func(gateway string) *subnets.Subnet {
		t.Helper()
		revised, err := subnets.Update(ctx, client, baseline.ID, subnets.UpdateOpts{GatewayIP: &gateway}).Extract()
		require.NoError(t, err)
		expected := *baseline
		expected.GatewayIP = gateway
		assert.Equal(t, &expected, revised)
		assert.Equal(t, revised, read())
		return revised
	}
	address := func(ip string) []ports.IP {
		return []ports.IP{{SubnetID: baseline.ID, IPAddress: ip}}
	}
	ordinary, err := ports.Create(ctx, client, ports.CreateOpts{
		NetworkID: fixtureNetworkID, FixedIPs: address(baseline.GatewayIP),
	}).Extract()
	require.NoError(t, err)
	require.Equal(t, address(baseline.GatewayIP), ordinary.FixedIPs)

	const current = "10.83.0.2"
	changed := update(current)
	stillOrdinary, err := ports.Get(ctx, client, ordinary.ID).Extract()
	require.NoError(t, err)
	assert.Equal(t, ordinary, stillOrdinary)
	router, err := routers.Create(ctx, client, routers.CreateOpts{Name: "owns-current-gateway"}).Extract()
	require.NoError(t, err)
	added, err := routers.AddInterface(ctx, client, router.ID, routers.AddInterfaceOpts{SubnetID: baseline.ID}).Extract()
	require.NoError(t, err)
	bound, err := ports.Get(ctx, client, added.PortID).Extract()
	require.NoError(t, err)
	require.Equal(t, address(current), bound.FixedIPs)
	assert.Equal(t, changed, update(current))

	// https://github.com/openstack/neutron/blob/master/neutron/db/db_base_plugin_v2.py
	for _, next := range []string{"10.83.0.3", ""} {
		_, err := subnets.Update(ctx, client, baseline.ID, subnets.UpdateOpts{GatewayIP: &next}).Extract()
		require.True(t, gophercloud.ResponseCodeIs(err, http.StatusConflict), "gateway %q: %v", next, err)
		var failure gophercloud.ErrUnexpectedResponseCode
		require.ErrorAs(t, err, &failure)
		assertPortNeutronError(t, failure.Body, "GatewayIpInUse")
		assert.Equal(t, changed, read())
	}
	stillBound, err := ports.Get(ctx, client, added.PortID).Extract()
	require.NoError(t, err)
	assert.Equal(t, bound, stillBound)

	_, err = routers.RemoveInterface(ctx, client, router.ID, routers.RemoveInterfaceOpts{SubnetID: baseline.ID}).Extract()
	require.NoError(t, err)
	update("10.83.0.3")
	update("192.0.2.1")
	cleared := update("")
	pages, err := subnets.List(client, subnets.ListOpts{NetworkID: fixtureNetworkID}).AllPages(ctx)
	require.NoError(t, err)
	listed, err := subnets.ExtractSubnets(pages)
	require.NoError(t, err)
	assert.Equal(t, []subnets.Subnet{*cleared}, listed)
	auto, err := ports.Create(ctx, client, ports.CreateOpts{NetworkID: fixtureNetworkID}).Extract()
	require.NoError(t, err)
	assert.Equal(t, address(pool[0].Start), auto.FixedIPs)
}

func TestSubnetGatewayUpdateWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	resources := createRouterInterfaceResources(t, provider)
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	path := subnetsPath + "/" + resources.subnetID
	status, headers, body := request(t, instance, http.MethodGet, path, "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	baseline := string(body)

	// https://docs.openstack.org/api-ref/network/v2/#update-subnet
	for _, invalid := range []struct {
		ip     string
		status int
		kind   string
	}{
		{"2001:db8::1", http.StatusBadRequest, "InvalidInput"},
		{"10.66.0.0", http.StatusBadRequest, "InvalidInput"},
		{"10.66.0.255", http.StatusBadRequest, "InvalidInput"},
		{"10.66.0.32", http.StatusConflict, "GatewayConflictWithAllocationPools"},
	} {
		payload := `{"subnet":{"name":"must-not-change","enable_dhcp":false,"gateway_ip":"` + invalid.ip + `"}}`
		status, headers, body := request(t, instance, http.MethodPut, path, payload, auth)
		require.Equal(t, invalid.status, status, "%s: %s", invalid.ip, body)
		requireJSON(t, headers)
		assertPortNeutronError(t, body, invalid.kind)
	}
	status, headers, body = request(t, instance, http.MethodGet, path, "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	assert.JSONEq(t, baseline, string(body))

	status, headers, body = request(t, instance, http.MethodPut, path, `{"subnet":{"gateway_ip":null}}`, auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	var cleared struct {
		Subnet map[string]json.RawMessage `json:"subnet"`
	}
	require.NoError(t, json.Unmarshal(body, &cleared))
	assert.JSONEq(t, `"`+resources.subnetID+`"`, string(cleared.Subnet["id"]))
	assert.JSONEq(t, "null", string(cleared.Subnet["gateway_ip"]))
	assert.JSONEq(t, `[{"start":"10.66.0.2","end":"10.66.0.254"}]`, string(cleared.Subnet["allocation_pools"]))
	assert.JSONEq(t, "true", string(cleared.Subnet["enable_dhcp"]))
}
