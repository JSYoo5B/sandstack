package server_test

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/networks"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/ports"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/subnets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const portsPath = networkPath + "/v2.0/ports"

func TestPortCreateAndDeleteWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	status, _, body := request(t, instance, http.MethodPost, networksPath,
		`{"network":{"name":"wire-port-network"}}`, auth)
	require.Equal(t, http.StatusCreated, status, "%s", body)
	var parent struct {
		Network wireNetwork `json:"network"`
	}
	require.NoError(t, json.Unmarshal(body, &parent))
	require.NotEmpty(t, parent.Network.ID)
	require.NotEmpty(t, parent.Network.ProjectID)
	networkID := parent.Network.ID
	status, _, body = request(t, instance, http.MethodPost, subnetsPath,
		fmt.Sprintf(`{"subnet":{"network_id":%q,"ip_version":4,"cidr":"10.42.0.0/30"}}`, networkID), auth)
	require.Equal(t, http.StatusCreated, status, "%s", body)
	var subnet struct {
		Subnet wireSubnet `json:"subnet"`
	}
	require.NoError(t, json.Unmarshal(body, &subnet))
	require.NotEmpty(t, subnet.Subnet.ID)

	// https://docs.openstack.org/api-ref/network/v2/#create-port
	const name = "wire-allocated-port"
	payload := fmt.Sprintf(`{"port":{"network_id":%q,"name":%q}}`, networkID, name)
	status, headers, body := request(t, instance, http.MethodPost, portsPath, payload, auth)

	require.Equal(t, http.StatusCreated, status, "%s", body)
	requireJSON(t, headers)
	var response struct {
		Port wirePort `json:"port"`
	}
	require.NoError(t, json.Unmarshal(body, &response))
	created := response.Port
	require.NotEmpty(t, created.ID)
	assert.Equal(t, networkID, created.NetworkID)
	assert.Equal(t, parent.Network.ProjectID, created.ProjectID)
	assert.Equal(t, parent.Network.ProjectID, created.TenantID)
	assert.Equal(t, name, created.Name)
	assert.Equal(t, "DOWN", created.Status)
	require.NotNil(t, created.AdminStateUp)
	assert.True(t, *created.AdminStateUp)
	require.NotNil(t, created.DeviceID)
	assert.Empty(t, *created.DeviceID)
	require.NotNil(t, created.DeviceOwner)
	assert.Empty(t, *created.DeviceOwner)
	assertLocalUnicastMAC(t, created.MACAddress)
	assert.Equal(t, []wirePortIP{{SubnetID: subnet.Subnet.ID, IPAddress: "10.42.0.2"}}, created.FixedIPs)

	for _, failure := range []struct {
		method  string
		path    string
		payload string
		status  int
		kind    string
	}{
		{http.MethodPost, portsPath, payload, http.StatusConflict, "IpAddressGenerationFailure"},
		{http.MethodDelete, networksPath + "/" + networkID, "", http.StatusConflict, "NetworkInUse"},
		{http.MethodDelete, subnetsPath + "/" + subnet.Subnet.ID, "", http.StatusConflict, "SubnetInUse"},
		{
			method:  http.MethodPost,
			path:    portsPath,
			payload: `{"port":{"network_id":"00000000-0000-0000-0000-000000000000","fixed_ips":[]}}`,
			status:  http.StatusNotFound,
			kind:    "NetworkNotFound",
		},
	} {
		status, headers, body := request(t, instance, failure.method, failure.path, failure.payload, auth)
		require.Equal(t, failure.status, status, "%s", body)
		requireJSON(t, headers)
		assertPortNeutronError(t, body, failure.kind)
	}

	status, _, body = request(t, instance, http.MethodGet, portsPath+"?network_id="+networkID, "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	var listing struct {
		Ports []wirePort `json:"ports"`
	}
	require.NoError(t, json.Unmarshal(body, &listing))
	assert.Equal(t, []wirePort{created}, listing.Ports)

	detail := portsPath + "/" + created.ID
	status, _, body = request(t, instance, http.MethodGet, detail, "", nil)
	assert.Equal(t, http.StatusUnauthorized, status, "%s", body)
	// https://docs.openstack.org/api-ref/network/v2/#delete-port
	status, _, body = request(t, instance, http.MethodDelete, detail, "", auth)
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	assert.Empty(t, body)
	status, headers, body = request(t, instance, http.MethodGet, detail, "", auth)
	require.Equal(t, http.StatusNotFound, status, "%s", body)
	requireJSON(t, headers)
	assertPortNeutronError(t, body, "PortNotFound")
}

func TestGophercloudAllocatesAndReleasesPortAddresses(t *testing.T) {
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
	parent, err := networks.Create(t.Context(), service, networks.CreateOpts{Name: "port-network"}).Extract()
	require.NoError(t, err)
	require.NotNil(t, parent)
	require.NotEmpty(t, parent.ID)
	subnet, err := subnets.Create(t.Context(), service, subnets.CreateOpts{
		NetworkID: parent.ID, CIDR: "10.45.0.0/30", IPVersion: gophercloud.IPv4,
	}).Extract()
	require.NoError(t, err)
	require.NotNil(t, subnet)
	require.NotEmpty(t, subnet.ID)
	createPort := func(opts ports.CreateOpts) *ports.Port {
		t.Helper()
		result, err := ports.Create(t.Context(), service, opts).Extract()
		require.NoError(t, err)
		require.NotNil(t, result)
		require.NotEmpty(t, result.ID)
		return result
	}
	listPorts := func(client *gophercloud.ServiceClient, networkID string) []ports.Port {
		t.Helper()
		pages, err := ports.List(client, ports.ListOpts{NetworkID: networkID}).AllPages(t.Context())
		require.NoError(t, err)
		result, err := ports.ExtractPorts(pages)
		require.NoError(t, err)
		return result
	}
	readPort := func(id string) *ports.Port {
		t.Helper()
		result, err := ports.Get(t.Context(), service, id).Extract()
		require.NoError(t, err)
		require.NotNil(t, result)
		return result
	}
	wantAddress := []ports.IP{{SubnetID: subnet.ID, IPAddress: "10.45.0.2"}}
	first := createPort(ports.CreateOpts{NetworkID: parent.ID, Name: "first"})
	assert.Equal(t, parent.ProjectID, first.ProjectID)
	assert.Equal(t, parent.ProjectID, first.TenantID)
	assert.Equal(t, "DOWN", first.Status)
	assert.True(t, first.AdminStateUp)
	assert.Equal(t, wantAddress, first.FixedIPs)
	assertLocalUnicastMAC(t, first.MACAddress)
	assert.Equal(t, first, readPort(first.ID))

	_, err = ports.Create(t.Context(), service, ports.CreateOpts{NetworkID: parent.ID}).Extract()
	require.Error(t, err)
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusConflict), "%v", err)
	assert.Equal(t, []ports.Port{*first}, listPorts(service, parent.ID))

	noIP := createPort(ports.CreateOpts{NetworkID: parent.ID, Name: "without-address", FixedIPs: []ports.IP{}})
	assert.Equal(t, []ports.IP{}, noIP.FixedIPs)
	assert.Equal(t, noIP, readPort(noIP.ID))
	other := createPort(ports.CreateOpts{NetworkID: fixtureNetworkID, FixedIPs: []ports.IP{}})
	assert.ElementsMatch(t, []ports.Port{*first, *noIP}, listPorts(service, parent.ID))
	assert.Equal(t, []ports.Port{*other}, listPorts(service, fixtureNetworkID))

	err = networks.Delete(t.Context(), service, parent.ID).ExtractErr()
	require.Error(t, err)
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusConflict), "%v", err)
	err = subnets.Delete(t.Context(), service, subnet.ID).ExtractErr()
	require.Error(t, err)
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusConflict), "%v", err)
	assert.ElementsMatch(t, []ports.Port{*first, *noIP}, listPorts(service, parent.ID))
	parentAfter, err := networks.Get(t.Context(), service, parent.ID).Extract()
	require.NoError(t, err)
	require.NotNil(t, parentAfter)
	assert.Equal(t, []string{subnet.ID}, parentAfter.Subnets)
	subnetAfter, err := subnets.Get(t.Context(), service, subnet.ID).Extract()
	require.NoError(t, err)
	assert.Equal(t, subnet, subnetAfter)

	require.NoError(t, ports.Delete(t.Context(), service, first.ID).ExtractErr())
	_, err = ports.Get(t.Context(), service, first.ID).Extract()
	require.Error(t, err)
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusNotFound), "%v", err)
	second := createPort(ports.CreateOpts{
		NetworkID: parent.ID, Name: "second", FixedIPs: []ports.IP{{SubnetID: subnet.ID}},
	})
	assert.Equal(t, wantAddress, second.FixedIPs)
	assert.NotEqual(t, first.ID, second.ID)
	assert.NotEqual(t, first.MACAddress, second.MACAddress)
	assertLocalUnicastMAC(t, second.MACAddress)
	require.NoError(t, ports.Delete(t.Context(), service, second.ID).ExtractErr())
	require.NoError(t, ports.Delete(t.Context(), service, noIP.ID).ExtractErr())
	require.NoError(t, networks.Delete(t.Context(), service, parent.ID).ExtractErr())
	_, err = subnets.Get(t.Context(), service, subnet.ID).Extract()
	require.Error(t, err)
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusNotFound), "%v", err)

	status, _, body := request(t, instance, http.MethodPost, "/_sandstack/reset", "", map[string]string{
		"X-SandStack-Admin-Token": adminToken,
	})
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	_, err = ports.Get(t.Context(), service, other.ID).Extract()
	require.Error(t, err)
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusUnauthorized), "%v", err)
	current := discover(authenticate(t, instance))
	assert.Empty(t, listPorts(current, ""))
	_, err = ports.Get(t.Context(), current, other.ID).Extract()
	require.Error(t, err)
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusNotFound), "%v", err)
}

func assertLocalUnicastMAC(t *testing.T, address string) {
	t.Helper()
	parsed, err := net.ParseMAC(address)
	require.NoError(t, err)
	require.Len(t, parsed, 6)
	assert.Equal(t, byte(2), parsed[0]&3)
}

func assertPortNeutronError(t *testing.T, body []byte, expectedType string) {
	t.Helper()
	var response struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"NeutronError"`
	}
	require.NoError(t, json.Unmarshal(body, &response))
	assert.Equal(t, expectedType, response.Error.Type)
	assert.NotEmpty(t, response.Error.Message)
}

type wirePort struct {
	ID           string       `json:"id"`
	NetworkID    string       `json:"network_id"`
	ProjectID    string       `json:"project_id"`
	TenantID     string       `json:"tenant_id"`
	Name         string       `json:"name"`
	Status       string       `json:"status"`
	AdminStateUp *bool        `json:"admin_state_up"`
	MACAddress   string       `json:"mac_address"`
	FixedIPs     []wirePortIP `json:"fixed_ips"`
	DeviceID     *string      `json:"device_id"`
	DeviceOwner  *string      `json:"device_owner"`
}

type wirePortIP struct {
	SubnetID  string `json:"subnet_id"`
	IPAddress string `json:"ip_address"`
}
