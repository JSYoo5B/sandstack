package server_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/networks"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/subnets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const subnetsPath = networkPath + "/v2.0/subnets"

func TestSubnetCreateAndDeleteWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	listing := func() []wireSubnet {
		t.Helper()
		status, headers, body := request(t, instance, http.MethodGet, subnetsPath, "", auth)
		require.Equal(t, http.StatusOK, status, "%s", body)
		requireJSON(t, headers)
		var response struct {
			Subnets []wireSubnet `json:"subnets"`
		}
		require.NoError(t, json.Unmarshal(body, &response))
		require.NotNil(t, response.Subnets)
		return response.Subnets
	}
	assert.Empty(t, listing())
	status, _, body := request(t, instance, http.MethodGet, networksPath+"/"+fixtureNetworkID, "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	var parent struct {
		Network wireNetwork `json:"network"`
	}
	require.NoError(t, json.Unmarshal(body, &parent))
	require.NotEmpty(t, parent.Network.ProjectID)

	const createBody = `{"subnet":{"network_id":"` + fixtureNetworkID + `",` +
		`"ip_version":4,"cidr":"10.42.0.0/24","name":"wire-subnet"}}`
	// https://docs.openstack.org/api-ref/network/v2/#create-subnet
	status, headers, body := request(t, instance, http.MethodPost, subnetsPath, createBody, auth)

	require.Equal(t, http.StatusCreated, status, "%s", body)
	requireJSON(t, headers)
	var created struct {
		Subnet wireSubnet `json:"subnet"`
	}
	require.NoError(t, json.Unmarshal(body, &created))
	current := created.Subnet
	require.NotEmpty(t, current.ID)
	assert.Equal(t, fixtureNetworkID, current.NetworkID)
	assert.Equal(t, parent.Network.ProjectID, current.ProjectID)
	assert.Equal(t, parent.Network.ProjectID, current.TenantID)
	assert.Equal(t, "wire-subnet", current.Name)
	assert.Equal(t, 4, current.IPVersion)
	assert.Equal(t, "10.42.0.0/24", current.CIDR)
	assert.Equal(t, "10.42.0.1", current.GatewayIP)
	require.NotNil(t, current.EnableDHCP)
	assert.True(t, *current.EnableDHCP)
	assert.Equal(t, []wireAllocationPool{{Start: "10.42.0.2", End: "10.42.0.254"}}, current.AllocationPools)
	assert.JSONEq(t, "[]", string(current.DNSNameservers))
	assert.JSONEq(t, "[]", string(current.HostRoutes))
	assert.Equal(t, []wireSubnet{current}, listing())

	for _, invalid := range []struct {
		name      string
		networkID string
		cidr      string
		ipVersion int
		status    int
		kind      string
	}{
		{"IPv6 outside fixture scope", fixtureNetworkID, "2001:db8::/64", 6, http.StatusBadRequest, "InvalidInput"},
		{"IPv6 CIDR with IPv4 version", fixtureNetworkID, "2001:db8::/64", 4, http.StatusBadRequest, "InvalidInput"},
		{"narrow prefix", fixtureNetworkID, "10.43.0.0/31", 4, http.StatusBadRequest, "InvalidInput"},
		{"same-network overlap", fixtureNetworkID, "10.42.0.128/25", 4, http.StatusBadRequest, "InvalidInput"},
		{"unknown parent", "00000000-0000-0000-0000-000000000000", "10.43.0.0/24", 4, http.StatusNotFound, "NetworkNotFound"},
	} {
		t.Run(invalid.name, func(t *testing.T) {
			payload := fmt.Sprintf(`{"subnet":{"network_id":%q,"cidr":%q,"ip_version":%d}}`,
				invalid.networkID, invalid.cidr, invalid.ipVersion)
			status, headers, body := request(t, instance, http.MethodPost, subnetsPath, payload, auth)

			require.Equal(t, invalid.status, status, "%s", body)
			requireJSON(t, headers)
			var response struct {
				Error struct {
					Type    string `json:"type"`
					Message string `json:"message"`
				} `json:"NeutronError"`
			}
			require.NoError(t, json.Unmarshal(body, &response))
			assert.Equal(t, invalid.kind, response.Error.Type)
			assert.NotEmpty(t, response.Error.Message)
		})
	}
	assert.Equal(t, []wireSubnet{current}, listing())

	detail := subnetsPath + "/" + current.ID
	status, _, body = request(t, instance, http.MethodGet, detail, "", nil)
	assert.Equal(t, http.StatusUnauthorized, status, "%s", body)
	// https://docs.openstack.org/api-ref/network/v2/#delete-subnet
	status, _, body = request(t, instance, http.MethodDelete, detail, "", auth)
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	assert.Empty(t, body)
	status, _, body = request(t, instance, http.MethodGet, detail, "", auth)
	assert.Equal(t, http.StatusNotFound, status, "%s", body)
	assert.Empty(t, listing())
}

func TestGophercloudManagesSubnetsThroughNetworkLifecycle(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	discover := func(provider *gophercloud.ProviderClient) *gophercloud.ServiceClient {
		t.Helper()
		client, err := openstack.NewNetworkV2(provider, gophercloud.EndpointOpts{
			Region: "RegionOne", Availability: gophercloud.AvailabilityPublic,
		})
		require.NoError(t, err)
		return client
	}
	service := discover(provider)
	parent, err := networks.Create(t.Context(), service, networks.CreateOpts{Name: "subnet-network"}).Extract()
	require.NoError(t, err)
	require.NotNil(t, parent)
	require.NotEmpty(t, parent.ID)
	listSubnets := func(client *gophercloud.ServiceClient) []subnets.Subnet {
		t.Helper()
		pages, err := subnets.List(client, nil).AllPages(t.Context())
		require.NoError(t, err)
		result, err := subnets.ExtractSubnets(pages)
		require.NoError(t, err)
		return result
	}
	readNetwork := func(client *gophercloud.ServiceClient, id string) *networks.Network {
		t.Helper()
		result, err := networks.Get(t.Context(), client, id).Extract()
		require.NoError(t, err)
		require.NotNil(t, result)
		return result
	}
	createSubnet := func(parentID, cidr, name string) *subnets.Subnet {
		t.Helper()
		result, err := subnets.Create(t.Context(), service, subnets.CreateOpts{
			NetworkID: parentID, CIDR: cidr, IPVersion: gophercloud.IPv4, Name: name,
		}).Extract()
		require.NoError(t, err)
		require.NotNil(t, result)
		require.NotEmpty(t, result.ID)
		return result
	}
	assert.Empty(t, listSubnets(service))
	assert.Equal(t, []string{}, parent.Subnets)

	created := createSubnet(parent.ID, "10.42.0.0/24", "sdk-subnet")
	assert.Equal(t, parent.ID, created.NetworkID)
	assert.Equal(t, parent.ProjectID, created.ProjectID)
	assert.Equal(t, "sdk-subnet", created.Name)
	assert.Equal(t, "10.42.0.0/24", created.CIDR)
	assert.Equal(t, "10.42.0.1", created.GatewayIP)
	assert.True(t, created.EnableDHCP)
	assert.Equal(t, []subnets.AllocationPool{{Start: "10.42.0.2", End: "10.42.0.254"}}, created.AllocationPools)
	view, err := subnets.Get(t.Context(), service, created.ID).Extract()
	require.NoError(t, err)
	assert.Equal(t, created, view)
	assert.Equal(t, []subnets.Subnet{*created}, listSubnets(service))
	assert.Equal(t, []string{created.ID}, readNetwork(service, parent.ID).Subnets)

	require.NoError(t, subnets.Delete(t.Context(), service, created.ID).ExtractErr())
	_, err = subnets.Get(t.Context(), service, created.ID).Extract()
	require.Error(t, err)
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusNotFound), "%v", err)
	assert.Equal(t, []string{}, readNetwork(service, parent.ID).Subnets)
	assert.Empty(t, listSubnets(service))

	cascading := createSubnet(parent.ID, "10.42.0.0/24", "cascading-subnet")
	remaining := createSubnet(fixtureNetworkID, "10.43.0.0/24", "seed-subnet")
	assert.ElementsMatch(t, []subnets.Subnet{*cascading, *remaining}, listSubnets(service))
	require.NoError(t, networks.Delete(t.Context(), service, parent.ID).ExtractErr())
	_, err = subnets.Get(t.Context(), service, cascading.ID).Extract()
	require.Error(t, err)
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusNotFound), "%v", err)
	assert.Equal(t, []subnets.Subnet{*remaining}, listSubnets(service))
	assert.Equal(t, []string{remaining.ID}, readNetwork(service, fixtureNetworkID).Subnets)

	status, _, body := request(t, instance, http.MethodPost, "/_sandstack/reset", "", map[string]string{
		"X-SandStack-Admin-Token": adminToken,
	})
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	_, err = subnets.Get(t.Context(), service, remaining.ID).Extract()
	require.Error(t, err)
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusUnauthorized), "%v", err)
	current := discover(authenticate(t, instance))
	assert.Equal(t, []string{}, readNetwork(current, fixtureNetworkID).Subnets)
	assert.Empty(t, listSubnets(current))
	_, err = subnets.Get(t.Context(), current, remaining.ID).Extract()
	require.Error(t, err)
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusNotFound), "%v", err)
}

type wireSubnet struct {
	ID              string               `json:"id"`
	NetworkID       string               `json:"network_id"`
	ProjectID       string               `json:"project_id"`
	TenantID        string               `json:"tenant_id"`
	Name            string               `json:"name"`
	IPVersion       int                  `json:"ip_version"`
	CIDR            string               `json:"cidr"`
	GatewayIP       string               `json:"gateway_ip"`
	EnableDHCP      *bool                `json:"enable_dhcp"`
	AllocationPools []wireAllocationPool `json:"allocation_pools"`
	DNSNameservers  json.RawMessage      `json:"dns_nameservers"`
	HostRoutes      json.RawMessage      `json:"host_routes"`
}

type wireAllocationPool struct {
	Start string `json:"start"`
	End   string `json:"end"`
}
