package server_test

import (
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

func TestGophercloudUsesExistingPortAsRouterInterface(t *testing.T) {
	instance := startServer(t, "")
	resources := createRouterInterfaceResources(t, authenticate(t, instance))
	service := resources.service
	routerID := resources.routerID
	ctx := t.Context()
	wrongSubnet, err := subnets.Create(ctx, service, subnets.CreateOpts{
		NetworkID: resources.networkID, CIDR: "10.67.0.0/24", IPVersion: gophercloud.IPv4,
	}).Extract()
	require.NoError(t, err)
	adminUp := false
	port, err := ports.Create(ctx, service, ports.CreateOpts{
		Name: "disabled-router-port", NetworkID: resources.networkID, AdminStateUp: &adminUp,
		FixedIPs: []ports.IP{{SubnetID: resources.subnetID}},
	}).Extract()
	require.NoError(t, err)
	require.Equal(t, []ports.IP{{SubnetID: resources.subnetID, IPAddress: "10.66.0.2"}}, port.FixedIPs)
	require.Equal(t, "disabled-router-port", port.Name)
	require.False(t, port.AdminStateUp)
	require.NotEmpty(t, port.MACAddress)
	require.Empty(t, port.DeviceID)
	require.Empty(t, port.DeviceOwner)

	noIP, err := ports.Create(ctx, service, ports.CreateOpts{
		NetworkID: resources.networkID, FixedIPs: []ports.IP{},
	}).Extract()
	require.NoError(t, err)
	_, err = routers.AddInterface(ctx, service, routerID, routers.AddInterfaceOpts{PortID: noIP.ID}).Extract()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusBadRequest), "bind unnumbered port: %v", err)
	unbound, err := ports.Get(ctx, service, noIP.ID).Extract()
	require.NoError(t, err)
	assert.Equal(t, noIP, unbound)

	add := routers.AddInterfaceOpts{PortID: port.ID}
	added, err := routers.AddInterface(ctx, service, routerID, add).Extract()

	require.NoError(t, err)
	assert.Equal(t, &routers.InterfaceInfo{
		ID: routerID, PortID: port.ID, SubnetID: resources.subnetID, TenantID: port.TenantID,
	}, added)
	wantBound := *port
	wantBound.DeviceID = routerID
	wantBound.DeviceOwner = "network:router_interface"
	bound, err := ports.Get(ctx, service, port.ID).Extract()
	require.NoError(t, err)
	assert.Equal(t, &wantBound, bound)

	_, err = routers.AddInterface(ctx, service, routerID, add).Extract()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusConflict), "bind an attached port: %v", err)
	err = ports.Delete(ctx, service, port.ID).ExtractErr()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusConflict), "direct delete of attached port: %v", err)
	_, err = routers.RemoveInterface(ctx, service, routerID, routers.RemoveInterfaceOpts{
		PortID: port.ID, SubnetID: wrongSubnet.ID,
	}).Extract()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusBadRequest), "detach with the wrong subnet: %v", err)
	remaining, err := ports.Get(ctx, service, port.ID).Extract()
	require.NoError(t, err)
	assert.Equal(t, &wantBound, remaining)

	remove := routers.RemoveInterfaceOpts{PortID: port.ID}
	removed, err := routers.RemoveInterface(ctx, service, routerID, remove).Extract()

	require.NoError(t, err)
	assert.Equal(t, added, removed)
	_, err = ports.Get(ctx, service, port.ID).Extract()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusNotFound), "read detached port: %v", err)
	_, err = routers.RemoveInterface(ctx, service, routerID, remove).Extract()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusNotFound), "repeat detach: %v", err)
	require.NoError(t, subnets.Delete(ctx, service, wrongSubnet.ID).ExtractErr())
	replacement, err := ports.Create(ctx, service, ports.CreateOpts{
		NetworkID: resources.networkID,
	}).Extract()
	require.NoError(t, err)
	assert.NotEqual(t, port.ID, replacement.ID)
	assert.Equal(t, port.FixedIPs, replacement.FixedIPs)

	readded, err := routers.AddInterface(ctx, service, routerID, routers.AddInterfaceOpts{
		PortID: replacement.ID,
	}).Extract()
	require.NoError(t, err)
	removed, err = routers.RemoveInterface(ctx, service, routerID, routers.RemoveInterfaceOpts{
		PortID: replacement.ID, SubnetID: resources.subnetID,
	}).Extract()
	require.NoError(t, err)
	assert.Equal(t, readded, removed)
	_, err = ports.Get(ctx, service, replacement.ID).Extract()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusNotFound), "read second detached port: %v", err)
}

func TestRouterPortInterfaceWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	projectID := routerProjectID(t, provider)
	resources := createRouterInterfaceResources(t, provider)
	port, err := ports.Create(t.Context(), resources.service, ports.CreateOpts{
		NetworkID: resources.networkID,
	}).Extract()
	require.NoError(t, err)
	require.NotEmpty(t, port.ID)
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	routerPath := routersPath + "/" + resources.routerID
	add := routerPath + "/add_router_interface"
	remove := routerPath + "/remove_router_interface"
	portOnly := fmt.Sprintf(`{"port_id":%q}`, port.ID)
	both := fmt.Sprintf(`{"port_id":%q,"subnet_id":%q}`, port.ID, resources.subnetID)
	want := fmt.Sprintf(`{
		"id":%q,"network_id":%q,"port_id":%q,
		"subnet_id":%q,"subnet_ids":[%q],"project_id":%q,"tenant_id":%q,"tags":[]
	}`, resources.routerID, resources.networkID, port.ID, resources.subnetID,
		resources.subnetID, projectID, projectID)

	// https://docs.openstack.org/api-ref/network/v2/#add-interface-to-router
	status, headers, body := request(t, instance, http.MethodPut, add, both, auth)
	require.Equal(t, http.StatusBadRequest, status, "%s", body)
	requireJSON(t, headers)
	assertPortNeutronError(t, body, "BadRequest")
	status, headers, body = request(t, instance, http.MethodPut, add, portOnly, auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	assert.JSONEq(t, want, string(body))
	status, headers, body = request(t, instance, http.MethodPut, add, portOnly, auth)
	require.Equal(t, http.StatusConflict, status, "%s", body)
	requireJSON(t, headers)
	assertPortNeutronError(t, body, "PortInUse")

	// https://docs.openstack.org/api-ref/network/v2/#remove-interface-from-router
	// Neutron raises SubnetMismatchForPort, a BadRequest, for mismatched selectors.
	wrongSubnet := fmt.Sprintf(`{"port_id":%q,"subnet_id":"00000000-0000-0000-0000-000000000000"}`, port.ID)
	status, headers, body = request(t, instance, http.MethodPut, remove, wrongSubnet, auth)
	require.Equal(t, http.StatusBadRequest, status, "%s", body)
	requireJSON(t, headers)
	assertPortNeutronError(t, body, "SubnetMismatchForPort")
	status, headers, body = request(t, instance, http.MethodPut, remove, both, auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	assert.JSONEq(t, want, string(body))
	status, headers, body = request(t, instance, http.MethodPut, remove, portOnly, auth)
	require.Equal(t, http.StatusNotFound, status, "%s", body)
	requireJSON(t, headers)
	assertPortNeutronError(t, body, "RouterInterfaceNotFound")
}
