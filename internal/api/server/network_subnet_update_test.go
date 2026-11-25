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

func TestGophercloudUpdatesSubnetMetadataWithoutChangingAllocations(t *testing.T) {
	instance := startServer(t, "")
	resources := createRouterInterfaceResources(t, authenticate(t, instance))
	ctx := t.Context()
	service := resources.service
	subnetID := resources.subnetID
	initial, err := subnets.Get(ctx, service, subnetID).Extract()
	require.NoError(t, err)
	require.True(t, initial.EnableDHCP)
	added, err := routers.AddInterface(ctx, service, resources.routerID, routers.AddInterfaceOpts{
		SubnetID: subnetID,
	}).Extract()
	require.NoError(t, err)
	interfacePort, err := ports.Get(ctx, service, added.PortID).Extract()
	require.NoError(t, err)
	assert.Equal(t, []ports.IP{{SubnetID: subnetID, IPAddress: initial.GatewayIP}}, interfacePort.FixedIPs)
	userPort, err := ports.Create(ctx, service, ports.CreateOpts{NetworkID: resources.networkID}).Extract()
	require.NoError(t, err)
	assert.Equal(t, []ports.IP{{SubnetID: subnetID, IPAddress: "10.66.0.2"}}, userPort.FixedIPs)

	name := "renamed-subnet"
	renamed, err := subnets.Update(ctx, service, subnetID, subnets.UpdateOpts{Name: &name}).Extract()

	require.NoError(t, err)
	want := *initial
	want.Name = name
	assert.Equal(t, &want, renamed)

	disabled := false
	updated, err := subnets.Update(ctx, service, subnetID, subnets.UpdateOpts{EnableDHCP: &disabled}).Extract()

	require.NoError(t, err)
	want.EnableDHCP = false
	assert.Equal(t, &want, updated)
	detail, err := subnets.Get(ctx, service, subnetID).Extract()
	require.NoError(t, err)
	assert.Equal(t, &want, detail)
	pages, err := subnets.List(service, nil).AllPages(ctx)
	require.NoError(t, err)
	listed, err := subnets.ExtractSubnets(pages)
	require.NoError(t, err)
	assert.Equal(t, []subnets.Subnet{want}, listed)
	for _, existing := range []*ports.Port{interfacePort, userPort} {
		current, err := ports.Get(ctx, service, existing.ID).Extract()
		require.NoError(t, err)
		assert.Equal(t, existing, current)
	}
	_, err = ports.Create(ctx, service, ports.CreateOpts{
		NetworkID: resources.networkID, FixedIPs: userPort.FixedIPs,
	}).Extract()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusConflict), "existing IP remains allocated: %v", err)

	empty := ""
	enabled := true
	cleared, err := subnets.Update(ctx, service, subnetID, subnets.UpdateOpts{Name: &empty, EnableDHCP: &enabled}).Extract()
	require.NoError(t, err)
	assert.Equal(t, initial, cleared)
}

func TestSubnetMetadataUpdateWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	resources := createRouterInterfaceResources(t, provider)
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	path := subnetsPath + "/" + resources.subnetID
	read := func(body []byte) wireSubnet {
		t.Helper()
		var response struct {
			Subnet wireSubnet `json:"subnet"`
		}
		require.NoError(t, json.Unmarshal(body, &response))
		require.Equal(t, resources.subnetID, response.Subnet.ID)
		require.NotNil(t, response.Subnet.EnableDHCP)
		return response.Subnet
	}
	status, headers, body := request(t, instance, http.MethodGet, path, "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	baseline := read(body)
	require.True(t, *baseline.EnableDHCP)
	const revise = `{"subnet":{"name":"wired-name","enable_dhcp":false}}`

	// https://docs.openstack.org/api-ref/network/v2/#update-subnet
	status, _, body = request(t, instance, http.MethodPut, path, revise, nil)
	require.Equal(t, http.StatusUnauthorized, status, "%s", body)
	status, headers, body = request(t, instance, http.MethodPut, path, revise, auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	want := baseline
	want.Name = "wired-name"
	disabled := false
	want.EnableDHCP = &disabled
	assert.Equal(t, want, read(body))

	status, headers, body = request(t, instance, http.MethodPut, path, `{"subnet":{"name":""}}`, auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	want.Name = ""
	assert.Equal(t, want, read(body))
	var textFields struct {
		Subnet map[string]json.RawMessage `json:"subnet"`
	}
	require.NoError(t, json.Unmarshal(body, &textFields))
	assert.JSONEq(t, `""`, string(textFields.Subnet["name"]))
	expected := string(body)

	status, headers, body = request(t, instance, http.MethodPut, path, `{"subnet":{}}`, auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	assert.JSONEq(t, expected, string(body))
	for _, invalid := range []string{
		`{"subnet":{"name":"must not change","cidr":"10.67.0.0/24"}}`,
		fmt.Sprintf(`{"subnet":{"network_id":%q}}`, resources.networkID),
		`{"subnet":{"ip_version":4}}`,
		`{"subnet":{"gateway_ip":"10.66.0.100"}}`,
		`{"subnet":{"allocation_pools":[]}}`,
		`{"subnet":{"name":null}}`,
		`{"subnet":{"enable_dhcp":null}}`,
		`{"subnet":{"enable_dhcp":"false"}}`,
		fmt.Sprintf(`{"subnet":{"project_id":%q}}`, baseline.ProjectID),
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
	status, headers, body = request(t, instance, http.MethodPut, subnetsPath+"/"+missing, revise, auth)
	require.Equal(t, http.StatusNotFound, status, "%s", body)
	requireJSON(t, headers)
	assertPortNeutronError(t, body, "SubnetNotFound")
}
