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

func TestGophercloudReplacesAllocationPoolsAndPreservesPorts(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	client, err := openstack.NewNetworkV2(provider, gophercloud.EndpointOpts{
		Region: "RegionOne", Availability: gophercloud.AvailabilityPublic,
	})
	require.NoError(t, err)
	ctx := t.Context()
	disabled := false
	created, err := subnets.Create(ctx, client, subnets.CreateOpts{
		Name: "pool-update", NetworkID: fixtureNetworkID, CIDR: "10.84.0.0/24",
		IPVersion: gophercloud.IPv4, EnableDHCP: &disabled,
		AllocationPools: []subnets.AllocationPool{{Start: "10.84.0.10", End: "10.84.0.11"}},
	}).Extract()
	require.NoError(t, err)
	require.NotEmpty(t, created.ID)
	address := func(ip string) []ports.IP {
		return []ports.IP{{SubnetID: created.ID, IPAddress: ip}}
	}
	auto := ports.CreateOpts{NetworkID: fixtureNetworkID}
	old, err := ports.Create(ctx, client, auto).Extract()
	require.NoError(t, err)
	require.Equal(t, address("10.84.0.10"), old.FixedIPs)
	reserved, err := ports.Create(ctx, client, ports.CreateOpts{
		NetworkID: fixtureNetworkID, FixedIPs: address("10.84.0.30"),
	}).Extract()
	require.NoError(t, err)
	require.Equal(t, address("10.84.0.30"), reserved.FixedIPs)

	pool := []subnets.AllocationPool{{Start: "10.84.0.30", End: "10.84.0.31"}}
	updated, err := subnets.Update(ctx, client, created.ID, subnets.UpdateOpts{AllocationPools: pool}).Extract()

	require.NoError(t, err)
	want := *created
	want.AllocationPools = pool
	assert.Equal(t, &want, updated)
	for _, port := range []*ports.Port{old, reserved} {
		found, err := ports.Get(ctx, client, port.ID).Extract()
		require.NoError(t, err)
		assert.Equal(t, port, found)
	}
	next, err := ports.Create(ctx, client, auto).Extract()
	require.NoError(t, err)
	assert.Equal(t, address("10.84.0.31"), next.FixedIPs)
	_, err = ports.Create(ctx, client, auto).Extract()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusConflict), "replacement pool exhausted: %v", err)

	name := "omit-pool-in-rename"
	renamed, err := subnets.Update(ctx, client, created.ID, subnets.UpdateOpts{Name: &name}).Extract()
	require.NoError(t, err)
	want.Name = name
	assert.Equal(t, &want, renamed)
	detail, err := subnets.Get(ctx, client, created.ID).Extract()
	require.NoError(t, err)
	assert.Equal(t, &want, detail)
	pages, err := subnets.List(client, subnets.ListOpts{NetworkID: fixtureNetworkID}).AllPages(ctx)
	require.NoError(t, err)
	listed, err := subnets.ExtractSubnets(pages)
	require.NoError(t, err)
	assert.Equal(t, []subnets.Subnet{want}, listed)
}

func TestSubnetAllocationPoolUpdateWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	resources := createRouterInterfaceResources(t, provider)
	ctx := t.Context()
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	path := subnetsPath + "/" + resources.subnetID
	read := func(body []byte) wireSubnet {
		t.Helper()
		var response struct {
			Subnet wireSubnet `json:"subnet"`
		}
		require.NoError(t, json.Unmarshal(body, &response))
		require.Equal(t, resources.subnetID, response.Subnet.ID)
		return response.Subnet
	}
	get := func() string {
		t.Helper()
		status, headers, body := request(t, instance, http.MethodGet, path, "", auth)
		require.Equal(t, http.StatusOK, status, "%s", body)
		requireJSON(t, headers)
		return string(body)
	}
	reject := func(payload, kind string) {
		t.Helper()
		status, headers, body := request(t, instance, http.MethodPut, path, payload, auth)
		require.Equal(t, http.StatusConflict, status, "%s", body)
		requireJSON(t, headers)
		assertPortNeutronError(t, body, kind)
	}
	baseline := get()
	const pool = `[{"start":"10.66.0.1","end":"10.66.0.3"}]`
	reject(`{"subnet":{"name":"must-not-change","gateway_ip":"10.66.0.40"}}`, "GatewayConflictWithAllocationPools")
	reject(`{"subnet":{"name":"must-not-change","allocation_pools":`+pool+`}}`, "GatewayConflictWithAllocationPools")
	assert.JSONEq(t, baseline, get())

	// The proposed gateway is checked against the proposed pools when both are supplied.
	// https://github.com/openstack/neutron/blob/master/neutron/db/db_base_plugin_v2.py#L969-L983
	const gatewayAndPool = `{"subnet":{"gateway_ip":"10.66.0.40","allocation_pools":` + pool + `}}`
	status, headers, body := request(t, instance, http.MethodPut, path, gatewayAndPool, auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	changed := read(body)
	assert.Equal(t, "10.66.0.40", changed.GatewayIP)
	assert.Equal(t, []wireAllocationPool{{Start: "10.66.0.1", End: "10.66.0.3"}}, changed.AllocationPools)
	current := string(body)
	reject(`{"subnet":{"name":"must-not-change","allocation_pools":[
		{"start":"10.66.0.6","end":"10.66.0.8"},{"start":"10.66.0.8","end":"10.66.0.10"}]}}`,
		"OverlappingAllocationPools")
	assert.JSONEq(t, current, get())

	attached, err := routers.AddInterface(ctx, resources.service, resources.routerID, routers.AddInterfaceOpts{
		SubnetID: resources.subnetID,
	}).Extract()
	require.NoError(t, err)
	reject(`{"subnet":{"name":"must-not-change","gateway_ip":"10.66.0.50",
		"allocation_pools":[{"start":"10.66.0.40","end":"10.66.0.42"}]}}`, "GatewayIpInUse")
	assert.JSONEq(t, current, get())
	port, err := ports.Get(ctx, resources.service, attached.PortID).Extract()
	require.NoError(t, err)
	assert.Equal(t, []ports.IP{{SubnetID: resources.subnetID, IPAddress: changed.GatewayIP}}, port.FixedIPs)
	_, err = routers.RemoveInterface(ctx, resources.service, resources.routerID, routers.RemoveInterfaceOpts{
		SubnetID: resources.subnetID,
	}).Extract()
	require.NoError(t, err)

	// https://docs.openstack.org/api-ref/network/v2/#update-subnet
	for _, clear := range []string{"[]", "null"} {
		status, headers, body := request(t, instance, http.MethodPut, path, gatewayAndPool, auth)
		require.Equal(t, http.StatusOK, status, "%s", body)
		requireJSON(t, headers)
		require.NotEmpty(t, read(body).AllocationPools)
		status, headers, body = request(t, instance, http.MethodPut, path,
			`{"subnet":{"allocation_pools":`+clear+`}}`, auth)
		require.Equal(t, http.StatusOK, status, "%s", body)
		requireJSON(t, headers)
		cleared := read(body)
		assert.Equal(t, []wireAllocationPool{}, cleared.AllocationPools)
		assert.Equal(t, changed.GatewayIP, cleared.GatewayIP)
		assert.JSONEq(t, string(body), get())
		_, err := ports.Create(ctx, resources.service, ports.CreateOpts{NetworkID: resources.networkID}).Extract()
		assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusConflict), "empty pool (%s): %v", clear, err)
		explicit, err := ports.Create(ctx, resources.service, ports.CreateOpts{
			NetworkID: resources.networkID, FixedIPs: []ports.IP{{IPAddress: "10.66.0.2"}},
		}).Extract()
		require.NoError(t, err)
		assert.Equal(t, []ports.IP{{SubnetID: resources.subnetID, IPAddress: "10.66.0.2"}}, explicit.FixedIPs)
		require.NoError(t, ports.Delete(ctx, resources.service, explicit.ID).ExtractErr())
	}
}
