package server_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/ports"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/subnets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGophercloudAllocatesPortsFromCustomSubnetPool(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	client, err := openstack.NewNetworkV2(provider, gophercloud.EndpointOpts{
		Region: "RegionOne", Availability: gophercloud.AvailabilityPublic,
	})
	require.NoError(t, err)
	ctx := t.Context()
	pool := []subnets.AllocationPool{{Start: "10.81.0.10", End: "10.81.0.11"}}
	disabled := false
	created, err := subnets.Create(ctx, client, subnets.CreateOpts{
		Name: "custom-pool", NetworkID: fixtureNetworkID, CIDR: "10.81.0.0/24",
		IPVersion: gophercloud.IPv4, EnableDHCP: &disabled, AllocationPools: pool,
	}).Extract()
	require.NoError(t, err)
	require.NotEmpty(t, created.ID)
	assert.Equal(t, "10.81.0.1", created.GatewayIP)
	assert.Equal(t, pool, created.AllocationPools)
	assert.False(t, created.EnableDHCP)
	detail, err := subnets.Get(ctx, client, created.ID).Extract()
	require.NoError(t, err)
	assert.Equal(t, created, detail)
	pages, err := subnets.List(client, subnets.ListOpts{NetworkID: fixtureNetworkID}).AllPages(ctx)
	require.NoError(t, err)
	listed, err := subnets.ExtractSubnets(pages)
	require.NoError(t, err)
	assert.Equal(t, []subnets.Subnet{*created}, listed)
	address := func(ip string) []ports.IP {
		return []ports.IP{{SubnetID: created.ID, IPAddress: ip}}
	}

	first, err := ports.Create(ctx, client, ports.CreateOpts{NetworkID: fixtureNetworkID}).Extract()
	require.NoError(t, err)
	assert.Equal(t, address("10.81.0.10"), first.FixedIPs)
	second, err := ports.Create(ctx, client, ports.CreateOpts{
		NetworkID: fixtureNetworkID, FixedIPs: []ports.IP{{SubnetID: created.ID}},
	}).Extract()
	require.NoError(t, err)
	assert.Equal(t, address("10.81.0.11"), second.FixedIPs)
	_, err = ports.Create(ctx, client, ports.CreateOpts{NetworkID: fixtureNetworkID}).Extract()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusConflict), "pool is exhausted: %v", err)

	// The IPAM driver checks a specifically requested address against the subnet.
	// https://github.com/openstack/neutron/blob/master/neutron/ipam/drivers/neutrondb_ipam/driver.py#L125-L141
	outsidePool, err := ports.Create(ctx, client, ports.CreateOpts{
		NetworkID: fixtureNetworkID, FixedIPs: address("10.81.0.2"),
	}).Extract()
	require.NoError(t, err)
	assert.Equal(t, address("10.81.0.2"), outsidePool.FixedIPs)
	_, err = ports.Create(ctx, client, ports.CreateOpts{NetworkID: fixtureNetworkID}).Extract()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusConflict), "explicit IP didn't enlarge pool: %v", err)

	require.NoError(t, ports.Delete(ctx, client, first.ID).ExtractErr())
	reused, err := ports.Create(ctx, client, ports.CreateOpts{NetworkID: fixtureNetworkID}).Extract()
	require.NoError(t, err)
	assert.NotEqual(t, first.ID, reused.ID)
	assert.Equal(t, first.FixedIPs, reused.FixedIPs)
}

func TestSubnetAllocationPoolWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	const cidr = "10.81.0.0/24"
	create := func(pool string) string {
		return fmt.Sprintf(`{"subnet":{"network_id":%q,"ip_version":4,"cidr":%q,"allocation_pools":%s}}`,
			fixtureNetworkID, cidr, pool)
	}

	// https://docs.openstack.org/api-ref/network/v2/#create-subnet
	for _, rejected := range []struct {
		name   string
		pool   string
		status int
		kind   string
	}{
		{"multiple pools", `[{"start":"10.81.0.10","end":"10.81.0.11"},{"start":"10.81.0.20","end":"10.81.0.21"}]`,
			http.StatusBadRequest, "HTTPBadRequest"},
		{"missing end", `[{"start":"10.81.0.10"}]`, http.StatusBadRequest, "HTTPBadRequest"},
		{"malformed address", `[{"start":"10.81.0.999","end":"10.81.0.11"}]`,
			http.StatusBadRequest, "HTTPBadRequest"},
		{"unknown key", `[{"start":"10.81.0.10","end":"10.81.0.11","name":"invalid"}]`,
			http.StatusBadRequest, "HTTPBadRequest"},
		{"reversed", `[{"start":"10.81.0.11","end":"10.81.0.10"}]`,
			http.StatusBadRequest, "InvalidAllocationPool"},
		{"different family", `[{"start":"2001:db8::1","end":"2001:db8::2"}]`,
			http.StatusBadRequest, "InvalidAllocationPool"},
		{"network address", `[{"start":"10.81.0.0","end":"10.81.0.1"}]`,
			http.StatusBadRequest, "OutOfBoundsAllocationPool"},
		{"broadcast address", `[{"start":"10.81.0.254","end":"10.81.0.255"}]`,
			http.StatusBadRequest, "OutOfBoundsAllocationPool"},
		{"outside CIDR", `[{"start":"10.82.0.10","end":"10.82.0.11"}]`,
			http.StatusBadRequest, "OutOfBoundsAllocationPool"},
		{"default gateway", `[{"start":"10.81.0.1","end":"10.81.0.2"}]`,
			http.StatusConflict, "GatewayConflictWithAllocationPools"},
	} {
		t.Run(rejected.name, func(t *testing.T) {
			status, headers, body := request(t, instance, http.MethodPost, subnetsPath, create(rejected.pool), auth)
			require.Equal(t, rejected.status, status, "%s", body)
			requireJSON(t, headers)
			assertPortNeutronError(t, body, rejected.kind)
		})
	}
	status, headers, body := request(t, instance, http.MethodGet, subnetsPath, "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	assert.JSONEq(t, `{"subnets":[]}`, string(body))

	readCreated := func(body []byte) wireSubnet {
		t.Helper()
		var response struct {
			Subnet wireSubnet `json:"subnet"`
		}
		require.NoError(t, json.Unmarshal(body, &response))
		require.NotEmpty(t, response.Subnet.ID)
		assert.Equal(t, fixtureNetworkID, response.Subnet.NetworkID)
		assert.Equal(t, "10.81.0.1", response.Subnet.GatewayIP)
		return response.Subnet
	}
	for _, defaults := range []string{"null", "[]"} {
		status, headers, body := request(t, instance, http.MethodPost, subnetsPath, create(defaults), auth)
		require.Equal(t, http.StatusCreated, status, "%s", body)
		requireJSON(t, headers)
		subnet := readCreated(body)
		assert.Equal(t, []wireAllocationPool{{Start: "10.81.0.2", End: "10.81.0.254"}}, subnet.AllocationPools)
		status, _, body = request(t, instance, http.MethodDelete, subnetsPath+"/"+subnet.ID, "", auth)
		require.Equal(t, http.StatusNoContent, status, "%s", body)
	}

	const oneAddress = `[{"start":"10.81.0.10","end":"10.81.0.10"}]`
	status, headers, body = request(t, instance, http.MethodPost, subnetsPath, create(oneAddress), auth)
	require.Equal(t, http.StatusCreated, status, "%s", body)
	requireJSON(t, headers)
	subnet := readCreated(body)
	assert.Equal(t, []wireAllocationPool{{Start: "10.81.0.10", End: "10.81.0.10"}}, subnet.AllocationPools)
}
