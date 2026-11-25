package server_test

import (
	"encoding/json"
	"fmt"
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

func TestGophercloudCreatesSubnetsWithExplicitGatewayModes(t *testing.T) {
	for _, test := range []struct {
		name       string
		gateway    string
		pools      []subnets.AllocationPool
		allocates  []string
		attachKind string
	}{
		{
			name:    "gateway within subnet",
			gateway: "10.82.0.3",
			pools: []subnets.AllocationPool{
				{Start: "10.82.0.1", End: "10.82.0.2"},
				{Start: "10.82.0.4", End: "10.82.0.6"},
			},
			allocates: []string{"10.82.0.1", "10.82.0.2", "10.82.0.4"},
		},
		{
			name:       "no gateway",
			pools:      []subnets.AllocationPool{{Start: "10.82.0.1", End: "10.82.0.6"}},
			allocates:  []string{"10.82.0.1"},
			attachKind: "BadRequest",
		},
		{
			name:       "gateway outside subnet",
			gateway:    "192.0.2.1",
			pools:      []subnets.AllocationPool{{Start: "10.82.0.1", End: "10.82.0.6"}},
			allocates:  []string{"10.82.0.1"},
			attachKind: "InvalidIpForSubnet",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			instance := startServer(t, "")
			provider := authenticate(t, instance)
			client, err := openstack.NewNetworkV2(provider, gophercloud.EndpointOpts{
				Region: "RegionOne", Availability: gophercloud.AvailabilityPublic,
			})
			require.NoError(t, err)
			ctx := t.Context()
			dhcp := false
			subnet, err := subnets.Create(ctx, client, subnets.CreateOpts{
				NetworkID: fixtureNetworkID, CIDR: "10.82.0.0/29", IPVersion: gophercloud.IPv4,
				GatewayIP: &test.gateway, EnableDHCP: &dhcp,
			}).Extract()
			require.NoError(t, err)
			require.NotEmpty(t, subnet.ID)
			assert.Equal(t, test.gateway, subnet.GatewayIP)
			assert.Equal(t, test.pools, subnet.AllocationPools)
			detail, err := subnets.Get(ctx, client, subnet.ID).Extract()
			require.NoError(t, err)
			assert.Equal(t, subnet, detail)

			router, err := routers.Create(ctx, client, routers.CreateOpts{Name: "gateway-router"}).Extract()
			require.NoError(t, err)
			interfaceInfo, err := routers.AddInterface(ctx, client, router.ID, routers.AddInterfaceOpts{
				SubnetID: subnet.ID,
			}).Extract()
			address := func(ip string) []ports.IP {
				return []ports.IP{{SubnetID: subnet.ID, IPAddress: ip}}
			}
			if test.attachKind == "" {
				require.NoError(t, err)
				require.NotEmpty(t, interfaceInfo.PortID)
				bound, err := ports.Get(ctx, client, interfaceInfo.PortID).Extract()
				require.NoError(t, err)
				assert.Equal(t, router.ID, bound.DeviceID)
				assert.Equal(t, routerInterfaceOwner, bound.DeviceOwner)
				assert.Equal(t, address(test.gateway), bound.FixedIPs)
			} else {
				require.True(t, gophercloud.ResponseCodeIs(err, http.StatusBadRequest), "%v", err)
				var unknown gophercloud.ErrUnexpectedResponseCode
				require.ErrorAs(t, err, &unknown)
				assertPortNeutronError(t, unknown.Body, test.attachKind)
				pages, err := ports.List(client, ports.ListOpts{NetworkID: fixtureNetworkID}).AllPages(ctx)
				require.NoError(t, err)
				listed, err := ports.ExtractPorts(pages)
				require.NoError(t, err)
				assert.Empty(t, listed)
				require.NoError(t, routers.Delete(ctx, client, router.ID).ExtractErr())
				get, err := subnets.Get(ctx, client, subnet.ID).Extract()
				require.NoError(t, err)
				assert.Equal(t, subnet, get)
			}
			for _, ip := range test.allocates {
				port, err := ports.Create(ctx, client, ports.CreateOpts{NetworkID: fixtureNetworkID}).Extract()
				require.NoError(t, err)
				assert.Equal(t, address(ip), port.FixedIPs)
			}
		})
	}
}

func TestSubnetGatewayCreateWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	create := func(fields string) string {
		return fmt.Sprintf(`{"subnet":{"network_id":%q,"cidr":"10.82.0.0/29","ip_version":4,%s}}`,
			fixtureNetworkID, fields)
	}

	// https://docs.openstack.org/api-ref/network/v2/#create-subnet
	for _, rejected := range []struct {
		name   string
		fields string
		status int
		kind   string
	}{
		{"wrong family", `"gateway_ip":"2001:db8::1"`, http.StatusBadRequest, "InvalidInput"},
		{"network address", `"gateway_ip":"10.82.0.0"`, http.StatusBadRequest, "InvalidInput"},
		{"broadcast address", `"gateway_ip":"10.82.0.7"`, http.StatusBadRequest, "InvalidInput"},
		{"malformed address", `"gateway_ip":"10.82.0.999"`, http.StatusBadRequest, ""},
		{"gateway within custom pool",
			`"gateway_ip":"10.82.0.3","allocation_pools":[{"start":"10.82.0.2","end":"10.82.0.4"}]`,
			http.StatusConflict, "GatewayConflictWithAllocationPools"},
	} {
		t.Run(rejected.name, func(t *testing.T) {
			status, headers, body := request(t, instance, http.MethodPost, subnetsPath, create(rejected.fields), auth)
			require.Equal(t, rejected.status, status, "%s", body)
			requireJSON(t, headers)
			if rejected.kind != "" {
				assertPortNeutronError(t, body, rejected.kind)
			}
		})
	}
	status, headers, body := request(t, instance, http.MethodGet, subnetsPath, "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	assert.JSONEq(t, `{"subnets":[]}`, string(body))

	status, headers, body = request(t, instance, http.MethodPost, subnetsPath, create(`"gateway_ip":null`), auth)
	require.Equal(t, http.StatusCreated, status, "%s", body)
	requireJSON(t, headers)
	var response struct {
		Subnet struct {
			ID      string          `json:"id"`
			Gateway json.RawMessage `json:"gateway_ip"`
			Pools   json.RawMessage `json:"allocation_pools"`
		} `json:"subnet"`
	}
	require.NoError(t, json.Unmarshal(body, &response))
	require.NotEmpty(t, response.Subnet.ID)
	assert.JSONEq(t, "null", string(response.Subnet.Gateway))
	assert.JSONEq(t, `[{"start":"10.82.0.1","end":"10.82.0.6"}]`, string(response.Subnet.Pools))
}
