package server_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/layer3/routers"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/networks"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/ports"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/subnets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGophercloudConnectsAndDisconnectsRouterSubnet(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	projectID := routerProjectID(t, provider)
	resources := createRouterInterfaceResources(t, provider)
	service := resources.service
	networkID, subnetID, routerID := resources.networkID, resources.subnetID, resources.routerID
	ctx := t.Context()
	otherRouter, err := routers.Create(ctx, service, routers.CreateOpts{Name: "second-router"}).Extract()
	require.NoError(t, err)
	require.NotEmpty(t, otherRouter.ID)
	subnet, err := subnets.Get(ctx, service, subnetID).Extract()
	require.NoError(t, err)
	require.Equal(t, "10.66.0.1", subnet.GatewayIP)

	add := routers.AddInterfaceOpts{SubnetID: subnetID}
	added, err := routers.AddInterface(ctx, service, routerID, add).Extract()

	require.NoError(t, err)
	require.NotEmpty(t, added.PortID)
	assert.Equal(t, routerID, added.ID)
	assert.Equal(t, subnetID, added.SubnetID)
	assert.Equal(t, projectID, added.TenantID)
	port, err := ports.Get(ctx, service, added.PortID).Extract()
	require.NoError(t, err)
	assert.Equal(t, added.PortID, port.ID)
	assert.Equal(t, networkID, port.NetworkID)
	assert.Equal(t, projectID, port.ProjectID)
	assert.Equal(t, projectID, port.TenantID)
	assert.Equal(t, routerID, port.DeviceID)
	assert.Equal(t, "network:router_interface", port.DeviceOwner)
	assert.Equal(t, []ports.IP{{SubnetID: subnetID, IPAddress: subnet.GatewayIP}}, port.FixedIPs)

	_, err = routers.AddInterface(ctx, service, routerID, add).Extract()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusBadRequest), "%v", err)
	_, err = routers.AddInterface(ctx, service, otherRouter.ID, add).Extract()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusConflict), "%v", err)
	err = ports.Delete(ctx, service, port.ID).ExtractErr()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusConflict), "delete port: %v", err)
	err = routers.Delete(ctx, service, routerID).ExtractErr()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusConflict), "delete router: %v", err)
	err = subnets.Delete(ctx, service, subnetID).ExtractErr()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusConflict), "delete subnet: %v", err)
	err = networks.Delete(ctx, service, networkID).ExtractErr()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusConflict), "delete network: %v", err)
	remainingPort, err := ports.Get(ctx, service, port.ID).Extract()
	require.NoError(t, err)
	assert.Equal(t, port, remainingPort)
	pages, err := ports.List(service, ports.ListOpts{NetworkID: networkID}).AllPages(ctx)
	require.NoError(t, err)
	listed, err := ports.ExtractPorts(pages)
	require.NoError(t, err)
	assert.Equal(t, []ports.Port{*port}, listed)

	remove := routers.RemoveInterfaceOpts{SubnetID: subnetID}
	removed, err := routers.RemoveInterface(ctx, service, routerID, remove).Extract()

	require.NoError(t, err)
	assert.Equal(t, added, removed)
	_, err = ports.Get(ctx, service, port.ID).Extract()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusNotFound), "%v", err)
	_, err = routers.RemoveInterface(ctx, service, routerID, remove).Extract()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusNotFound), "%v", err)
	require.NoError(t, routers.Delete(ctx, service, routerID).ExtractErr())
	require.NoError(t, routers.Delete(ctx, service, otherRouter.ID).ExtractErr())
	require.NoError(t, subnets.Delete(ctx, service, subnetID).ExtractErr())
	require.NoError(t, networks.Delete(ctx, service, networkID).ExtractErr())
}

func TestRouterInterfaceWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	projectID := routerProjectID(t, provider)
	resources := createRouterInterfaceResources(t, provider)
	networkID, subnetID, routerID := resources.networkID, resources.subnetID, resources.routerID
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	path := routersPath + "/" + routerID
	add := path + "/add_router_interface"
	remove := path + "/remove_router_interface"
	payload := fmt.Sprintf(`{"subnet_id":%q}`, subnetID)
	const missing = "00000000-0000-0000-0000-000000000000"

	status, _, body := request(t, instance, http.MethodPut, add, payload, nil)
	assert.Equal(t, http.StatusUnauthorized, status, "%s", body)
	for _, invalid := range []struct {
		name   string
		path   string
		body   string
		status int
		kind   string
	}{
		{"missing selector", add, `{}`, http.StatusBadRequest, "BadRequest"},
		{"invalid subnet ID", add, `{"subnet_id":"invalid"}`, http.StatusBadRequest, "BadRequest"},
		{"port selector", add, fmt.Sprintf(`{"port_id":%q}`, missing), http.StatusBadRequest, "BadRequest"},
		{"missing subnet", add, fmt.Sprintf(`{"subnet_id":%q}`, missing), http.StatusNotFound, "SubnetNotFound"},
		{"missing router", routersPath + "/" + missing + "/add_router_interface", payload, http.StatusNotFound,
			"RouterNotFound"},
	} {
		t.Run(invalid.name, func(t *testing.T) {
			status, headers, body := request(t, instance, http.MethodPut, invalid.path, invalid.body, auth)
			require.Equal(t, invalid.status, status, "%s", body)
			requireJSON(t, headers)
			assertPortNeutronError(t, body, invalid.kind)
		})
	}

	// https://docs.openstack.org/api-ref/network/v2/#add-interface-to-router
	status, headers, body := request(t, instance, http.MethodPut, add, payload, auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	var added, removed struct {
		ID        string   `json:"id"`
		NetworkID string   `json:"network_id"`
		PortID    string   `json:"port_id"`
		SubnetID  string   `json:"subnet_id"`
		SubnetIDs []string `json:"subnet_ids"`
		ProjectID string   `json:"project_id"`
		TenantID  string   `json:"tenant_id"`
		Tags      []string `json:"tags"`
	}
	require.NoError(t, json.Unmarshal(body, &added))
	require.NotEmpty(t, added.PortID)
	assert.Equal(t, routerID, added.ID)
	assert.Equal(t, networkID, added.NetworkID)
	assert.Equal(t, subnetID, added.SubnetID)
	assert.Equal(t, []string{subnetID}, added.SubnetIDs)
	assert.Equal(t, projectID, added.ProjectID)
	assert.Equal(t, projectID, added.TenantID)
	assert.Equal(t, []string{}, added.Tags)

	status, headers, body = request(t, instance, http.MethodDelete, portsPath+"/"+added.PortID, "", auth)
	require.Equal(t, http.StatusConflict, status, "%s", body)
	requireJSON(t, headers)
	assertPortNeutronError(t, body, "ServicePortInUse")
	status, _, body = request(t, instance, http.MethodPut, remove, payload, nil)
	assert.Equal(t, http.StatusUnauthorized, status, "%s", body)

	// https://docs.openstack.org/api-ref/network/v2/#remove-interface-from-router
	status, headers, body = request(t, instance, http.MethodPut, remove, payload, auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	require.NoError(t, json.Unmarshal(body, &removed))
	assert.Equal(t, added, removed)
	status, headers, body = request(t, instance, http.MethodPut, remove, payload, auth)
	require.Equal(t, http.StatusNotFound, status, "%s", body)
	requireJSON(t, headers)
	assertPortNeutronError(t, body, "RouterInterfaceNotFoundForSubnet")
}

type routerInterfaceResources struct {
	service   *gophercloud.ServiceClient
	networkID string
	subnetID  string
	routerID  string
}

func createRouterInterfaceResources(t *testing.T, provider *gophercloud.ProviderClient) routerInterfaceResources {
	t.Helper()
	service, err := openstack.NewNetworkV2(provider, gophercloud.EndpointOpts{
		Region: "RegionOne", Availability: gophercloud.AvailabilityPublic,
	})
	require.NoError(t, err)
	network, err := networks.Create(t.Context(), service, networks.CreateOpts{Name: "router-network"}).Extract()
	require.NoError(t, err)
	require.NotEmpty(t, network.ID)
	subnet, err := subnets.Create(t.Context(), service, subnets.CreateOpts{
		NetworkID: network.ID, CIDR: "10.66.0.0/24", IPVersion: gophercloud.IPv4,
	}).Extract()
	require.NoError(t, err)
	require.NotEmpty(t, subnet.ID)
	router, err := routers.Create(t.Context(), service, routers.CreateOpts{Name: "first-router"}).Extract()
	require.NoError(t, err)
	require.NotEmpty(t, router.ID)
	return routerInterfaceResources{
		service: service, networkID: network.ID, subnetID: subnet.ID, routerID: router.ID,
	}
}
