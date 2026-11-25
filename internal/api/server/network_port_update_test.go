package server_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/layer3/routers"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/ports"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGophercloudUpdatesAndReleasesPortAddresses(t *testing.T) {
	instance := startServer(t, "")
	resources := createRouterInterfaceResources(t, authenticate(t, instance))
	service := resources.service
	ctx := t.Context()
	address := func(ip string) []ports.IP {
		return []ports.IP{{SubnetID: resources.subnetID, IPAddress: ip}}
	}
	create := func(name, ip string) *ports.Port {
		t.Helper()
		port, err := ports.Create(ctx, service, ports.CreateOpts{
			NetworkID: resources.networkID, Name: name, FixedIPs: address(ip),
		}).Extract()
		require.NoError(t, err)
		require.NotEmpty(t, port.ID)
		return port
	}
	get := func(id string) *ports.Port {
		t.Helper()
		port, err := ports.Get(ctx, service, id).Extract()
		require.NoError(t, err)
		return port
	}
	current := create("before-update", "10.66.0.2")
	occupied := create("holds-address", "10.66.0.3")
	rename := "after-update"
	disabled := false

	_, err := ports.Update(ctx, service, current.ID, ports.UpdateOpts{
		Name: &rename, AdminStateUp: &disabled, FixedIPs: address("10.66.0.3"),
	}).Extract()

	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusConflict), "replace with occupied address: %v", err)
	assert.Equal(t, current, get(current.ID))
	assert.Equal(t, occupied, get(occupied.ID))
	_, err = ports.Create(ctx, service, ports.CreateOpts{
		NetworkID: resources.networkID, FixedIPs: address("10.66.0.2"),
	}).Extract()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusConflict), "old IP remains allocated: %v", err)

	revised, err := ports.Update(ctx, service, current.ID, ports.UpdateOpts{
		Name: &rename, AdminStateUp: &disabled, FixedIPs: []ports.IP{{IPAddress: "10.66.0.50"}},
	}).Extract()

	require.NoError(t, err)
	want := *current
	want.Name, want.AdminStateUp, want.FixedIPs = rename, disabled, address("10.66.0.50")
	assert.Equal(t, &want, revised)
	reclaimed := create("reuses-old-address", "10.66.0.2")
	assert.Equal(t, current.FixedIPs, reclaimed.FixedIPs)

	revised, err = ports.Update(ctx, service, current.ID, ports.UpdateOpts{
		FixedIPs: []ports.IP{{SubnetID: resources.subnetID}},
	}).Extract()
	require.NoError(t, err)
	assert.Equal(t, &want, revised)
	clearName := ""
	revised, err = ports.Update(ctx, service, current.ID, ports.UpdateOpts{Name: &clearName}).Extract()
	require.NoError(t, err)
	want.Name = ""
	assert.Equal(t, &want, revised)
	assert.Equal(t, &want, get(current.ID))
	pages, err := ports.List(service, ports.ListOpts{NetworkID: resources.networkID}).AllPages(ctx)
	require.NoError(t, err)
	listed, err := ports.ExtractPorts(pages)
	require.NoError(t, err)
	assert.ElementsMatch(t, []ports.Port{want, *occupied, *reclaimed}, listed)

	revised, err = ports.Update(ctx, service, current.ID, ports.UpdateOpts{FixedIPs: []ports.IP{}}).Extract()

	require.NoError(t, err)
	want.FixedIPs = []ports.IP{}
	assert.Equal(t, &want, revised)
	assert.Equal(t, &want, get(current.ID))
	released := create("reuses-replacement-address", "10.66.0.50")
	assert.Equal(t, address("10.66.0.50"), released.FixedIPs)
}

func TestPortUpdateWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	resources := createRouterInterfaceResources(t, provider)
	ctx := t.Context()
	service := resources.service
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	port, err := ports.Create(ctx, service, ports.CreateOpts{NetworkID: resources.networkID}).Extract()
	require.NoError(t, err)
	require.NotEmpty(t, port.ID)
	path := portsPath + "/" + port.ID

	// https://docs.openstack.org/api-ref/network/v2/#update-port
	const revise = `{"port":{"name":"wire-update","admin_state_up":false,"fixed_ips":[{"ip_address":"10.66.0.42"}]}}`
	status, _, body := request(t, instance, http.MethodPut, path, revise, nil)
	require.Equal(t, http.StatusUnauthorized, status, "%s", body)
	status, headers, body := request(t, instance, http.MethodPut, path, revise, auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	var response struct {
		Port wirePort `json:"port"`
	}
	require.NoError(t, json.Unmarshal(body, &response))
	revised := response.Port
	assert.Equal(t, port.ID, revised.ID)
	assert.Equal(t, port.NetworkID, revised.NetworkID)
	assert.Equal(t, port.ProjectID, revised.ProjectID)
	assert.Equal(t, port.TenantID, revised.TenantID)
	assert.Equal(t, port.MACAddress, revised.MACAddress)
	assert.Equal(t, "wire-update", revised.Name)
	require.NotNil(t, revised.AdminStateUp)
	assert.False(t, *revised.AdminStateUp)
	assert.Equal(t, []wirePortIP{{SubnetID: resources.subnetID, IPAddress: "10.66.0.42"}}, revised.FixedIPs)
	expected := string(body)

	for _, invalid := range []string{
		`{"port":{"fixed_ips":null}}`,
		`{"port":{"fixed_ips":[{}]}}`,
		`{"port":{"fixed_ips":[{"ip_address":"10.66.0.50"},{"ip_address":"10.66.0.51"}]}}`,
		`{"port":{"fixed_ips":[{"ip_address":"2001:db8::1"}]}}`,
		`{"port":{"name":"must not change","device_owner":"network:router_interface"}}`,
		fmt.Sprintf(`{"port":{"network_id":%q}}`, resources.networkID),
		`{"port":{"name":null}}`,
		`{"port":{"admin_state_up":null}}`,
	} {
		status, headers, body := request(t, instance, http.MethodPut, path, invalid, auth)
		require.Equal(t, http.StatusBadRequest, status, "%s: %s", invalid, body)
		requireJSON(t, headers)
		assertPortNeutronError(t, body, "BadRequest")
	}
	status, headers, body = request(t, instance, http.MethodGet, path, "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	assert.JSONEq(t, expected, string(body))

	const missing = "00000000-0000-0000-0000-000000000000"
	status, headers, body = request(t, instance, http.MethodPut, portsPath+"/"+missing, revise, auth)
	require.Equal(t, http.StatusNotFound, status, "%s", body)
	requireJSON(t, headers)
	assertPortNeutronError(t, body, "PortNotFound")

	interfaceInfo, err := routers.AddInterface(ctx, service, resources.routerID, routers.AddInterfaceOpts{
		SubnetID: resources.subnetID,
	}).Extract()
	require.NoError(t, err)
	managedPath := portsPath + "/" + interfaceInfo.PortID
	status, headers, body = request(t, instance, http.MethodPut, managedPath, `{"port":{"fixed_ips":[]}}`, auth)
	require.Equal(t, http.StatusConflict, status, "%s", body)
	requireJSON(t, headers)
	assertPortNeutronError(t, body, "ServicePortInUse")
	managed, err := ports.Get(ctx, service, interfaceInfo.PortID).Extract()
	require.NoError(t, err)
	assert.Equal(t, resources.routerID, managed.DeviceID)
	assert.Equal(t, routerInterfaceOwner, managed.DeviceOwner)
	assert.Equal(t, []ports.IP{{SubnetID: resources.subnetID, IPAddress: "10.66.0.1"}}, managed.FixedIPs)
}
