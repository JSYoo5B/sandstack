package server_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/layer3/routers"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/ports"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/subnets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGophercloudRequestsAndReusesFixedIPv4Ports(t *testing.T) {
	instance := startServer(t, "")
	resources := createRouterInterfaceResources(t, authenticate(t, instance))
	service := resources.service
	ctx := t.Context()
	create := func(fixedIPs []ports.IP) *ports.Port {
		t.Helper()
		port, err := ports.Create(ctx, service, ports.CreateOpts{
			NetworkID: resources.networkID, FixedIPs: fixedIPs,
		}).Extract()
		require.NoError(t, err)
		require.NotEmpty(t, port.ID)
		return port
	}
	list := func() []ports.Port {
		t.Helper()
		pages, err := ports.List(service, ports.ListOpts{NetworkID: resources.networkID}).AllPages(ctx)
		require.NoError(t, err)
		listed, err := ports.ExtractPorts(pages)
		require.NoError(t, err)
		return listed
	}

	requested := []ports.IP{{SubnetID: resources.subnetID, IPAddress: "10.66.0.2"}}
	first := create(requested)
	assert.Equal(t, requested, first.FixedIPs)
	automatic, err := ports.Create(ctx, service, ports.CreateOpts{NetworkID: resources.networkID}).Extract()
	require.NoError(t, err)
	assert.Equal(t, []ports.IP{{SubnetID: resources.subnetID, IPAddress: "10.66.0.3"}}, automatic.FixedIPs)

	other, err := subnets.Create(ctx, service, subnets.CreateOpts{
		NetworkID: resources.networkID, CIDR: "10.67.0.0/24", IPVersion: gophercloud.IPv4,
	}).Extract()
	require.NoError(t, err)
	require.NotEmpty(t, other.ID)
	inferred := create([]ports.IP{{IPAddress: "10.67.0.50"}})
	assert.Equal(t, []ports.IP{{SubnetID: other.ID, IPAddress: "10.67.0.50"}}, inferred.FixedIPs)
	detail, err := ports.Get(ctx, service, inferred.ID).Extract()
	require.NoError(t, err)
	assert.Equal(t, inferred, detail)

	for _, rejected := range []struct {
		name string
		ip   ports.IP
		code int
	}{
		{"occupied inferred address", ports.IP{IPAddress: "10.66.0.2"}, http.StatusConflict},
		{"wrong subnet", ports.IP{SubnetID: resources.subnetID, IPAddress: "10.67.0.51"}, http.StatusBadRequest},
		{"no matching network subnet", ports.IP{IPAddress: "10.68.0.2"}, http.StatusBadRequest},
		{"network address", ports.IP{SubnetID: resources.subnetID, IPAddress: "10.66.0.0"}, http.StatusBadRequest},
		{"broadcast address", ports.IP{SubnetID: resources.subnetID, IPAddress: "10.66.0.255"}, http.StatusBadRequest},
	} {
		_, err := ports.Create(ctx, service, ports.CreateOpts{
			NetworkID: resources.networkID, FixedIPs: []ports.IP{rejected.ip},
		}).Extract()
		assert.True(t, gophercloud.ResponseCodeIs(err, rejected.code), "%s: %v", rejected.name, err)
	}
	assert.ElementsMatch(t, []ports.Port{*first, *automatic, *inferred}, list())

	require.NoError(t, ports.Delete(ctx, service, first.ID).ExtractErr())
	_, err = ports.Get(ctx, service, first.ID).Extract()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusNotFound), "%v", err)
	reused := create(requested)
	assert.NotEqual(t, first.ID, reused.ID)
	assert.Equal(t, requested, reused.FixedIPs)
	assert.ElementsMatch(t, []ports.Port{*reused, *automatic, *inferred}, list())

	gateway := []ports.IP{{SubnetID: resources.subnetID, IPAddress: "10.66.0.1"}}
	gatewayPort := create(gateway)
	assert.Equal(t, gateway, gatewayPort.FixedIPs)
	attach := routers.AddInterfaceOpts{SubnetID: resources.subnetID}
	_, err = routers.AddInterface(ctx, service, resources.routerID, attach).Extract()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusConflict), "occupied gateway: %v", err)
	require.NoError(t, ports.Delete(ctx, service, gatewayPort.ID).ExtractErr())
	added, err := routers.AddInterface(ctx, service, resources.routerID, attach).Extract()
	require.NoError(t, err)
	interfacePort, err := ports.Get(ctx, service, added.PortID).Extract()
	require.NoError(t, err)
	assert.Equal(t, gateway, interfacePort.FixedIPs)
}

func TestFixedIPv4PortCreateWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	resources := createRouterInterfaceResources(t, provider)
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	payload := func(fixedIPs string) string {
		return fmt.Sprintf(`{"port":{"network_id":%q,"fixed_ips":%s}}`, resources.networkID, fixedIPs)
	}
	const address = "10.66.0.50"
	valid := payload(fmt.Sprintf(`[{"ip_address":%q}]`, address))

	// https://docs.openstack.org/api-ref/network/v2/#create-port
	status, headers, body := request(t, instance, http.MethodPost, portsPath, valid, auth)
	require.Equal(t, http.StatusCreated, status, "%s", body)
	requireJSON(t, headers)
	var created struct {
		Port struct {
			ID        string          `json:"id"`
			NetworkID string          `json:"network_id"`
			FixedIPs  json.RawMessage `json:"fixed_ips"`
		} `json:"port"`
	}
	require.NoError(t, json.Unmarshal(body, &created))
	require.NotEmpty(t, created.Port.ID)
	assert.Equal(t, resources.networkID, created.Port.NetworkID)
	assert.JSONEq(t, fmt.Sprintf(`[{"subnet_id":%q,"ip_address":%q}]`, resources.subnetID, address),
		string(created.Port.FixedIPs))

	for _, rejected := range []struct {
		name     string
		fixedIPs string
		status   int
		kind     string
	}{
		{"occupied", `[{"ip_address":"10.66.0.50"}]`, http.StatusConflict, "IpAddressAlreadyAllocated"},
		{"no matching subnet", `[{"ip_address":"10.68.0.50"}]`, http.StatusBadRequest, "InvalidIpForNetwork"},
		{"broadcast address", fmt.Sprintf(`[{"subnet_id":%q,"ip_address":"10.66.0.255"}]`, resources.subnetID),
			http.StatusBadRequest, "InvalidIpForSubnet"},
		{"IPv6", `[{"ip_address":"2001:db8::7"}]`, http.StatusBadRequest, "BadRequest"},
		{"malformed address", `[{"ip_address":"10.66.0.999"}]`, http.StatusBadRequest, "BadRequest"},
		{"null address", `[{"ip_address":null}]`, http.StatusBadRequest, "BadRequest"},
		{"missing selector", `[{}]`, http.StatusBadRequest, "BadRequest"},
		{"multiple", `[{"ip_address":"10.66.0.51"},{"ip_address":"10.66.0.52"}]`,
			http.StatusBadRequest, "BadRequest"},
		{"duplicate key", `[{"ip_address":"10.66.0.51","ip_address":"10.66.0.52"}]`,
			http.StatusBadRequest, "BadRequest"},
	} {
		t.Run(rejected.name, func(t *testing.T) {
			status, headers, body := request(t, instance, http.MethodPost, portsPath, payload(rejected.fixedIPs), auth)
			require.Equal(t, rejected.status, status, "%s", body)
			requireJSON(t, headers)
			assertPortNeutronError(t, body, rejected.kind)
		})
	}
}
