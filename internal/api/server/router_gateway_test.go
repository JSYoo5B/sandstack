package server_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/external"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/layer3/routers"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/networks"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/subnets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	externalNetworkID = "6c5a1e4f-0d3b-4f0e-9a76-2b8f1c3d9e10"
	externalSubnetID  = "9e7d3c2b-1a0f-4e8d-b7c6-5a4f3e2d1c0b"
	externalProjectID = "d0c1b2a39e8f4c7d8b6a5f4e3d2c1b0a"
)

type externalNetwork struct {
	networks.Network
	external.NetworkExternalExt
}

func privateNetworks() networks.ListOptsBuilder {
	internal := false
	return external.ListOptsExt{ListOptsBuilder: networks.ListOpts{}, External: &internal}
}

func TestGophercloudSetsRouterExternalGateway(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	resources := createRouterInterfaceResources(t, provider)
	client := resources.service
	ctx := t.Context()
	isExternal := true

	// https://docs.openstack.org/api-ref/network/v2/#list-networks
	pages, err := networks.List(client, external.ListOptsExt{
		ListOptsBuilder: networks.ListOpts{}, External: &isExternal,
	}).AllPages(ctx)
	require.NoError(t, err)
	var discovered []externalNetwork
	require.NoError(t, networks.ExtractNetworksInto(pages, &discovered))
	require.Len(t, discovered, 1)
	public := discovered[0]

	assert.Equal(t, externalNetworkID, public.ID)
	assert.Equal(t, "public", public.Name)
	assert.True(t, public.External)
	assert.False(t, public.Shared)
	assert.Equal(t, externalProjectID, public.ProjectID)
	assert.Equal(t, []string{externalSubnetID}, public.Subnets)
	name := "taken"
	_, err = networks.Update(ctx, client, public.ID, networks.UpdateOpts{Name: &name}).Extract()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusForbidden), "%v", err)
	err = networks.Delete(ctx, client, public.ID).ExtractErr()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusForbidden), "%v", err)

	// https://docs.openstack.org/api-ref/network/v2/#update-router
	gateway := routers.GatewayInfo{NetworkID: public.ID}
	updated, err := routers.Update(ctx, client, resources.routerID, routers.UpdateOpts{GatewayInfo: &gateway}).Extract()
	require.NoError(t, err)
	_, err = routers.AddInterface(ctx, client, resources.routerID, routers.AddInterfaceOpts{
		SubnetID: resources.subnetID,
	}).Extract()
	require.NoError(t, err)
	read, err := routers.Get(ctx, client, resources.routerID).Extract()
	require.NoError(t, err)

	require.NotNil(t, updated.GatewayInfo.EnableSNAT)
	assert.True(t, *updated.GatewayInfo.EnableSNAT)
	assert.Equal(t, public.ID, updated.GatewayInfo.NetworkID)
	assert.Equal(t, []routers.ExternalFixedIP{{IPAddress: "203.0.113.2", SubnetID: externalSubnetID}},
		updated.GatewayInfo.ExternalFixedIPs)
	assert.Equal(t, updated.GatewayInfo, read.GatewayInfo)

	// https://docs.openstack.org/api-ref/network/v2/#create-router
	second, err := routers.Create(ctx, client, routers.CreateOpts{Name: "second", GatewayInfo: &gateway}).Extract()
	require.NoError(t, err)
	assert.Equal(t, []routers.ExternalFixedIP{{IPAddress: "203.0.113.3", SubnetID: externalSubnetID}},
		second.GatewayInfo.ExternalFixedIPs)

	cleared, err := routers.Update(ctx, client, second.ID, routers.UpdateOpts{
		GatewayInfo: &routers.GatewayInfo{},
	}).Extract()
	require.NoError(t, err)
	assert.Equal(t, routers.GatewayInfo{}, cleared.GatewayInfo)
	require.NoError(t, routers.Delete(ctx, client, second.ID).ExtractErr())
	third, err := routers.Create(ctx, client, routers.CreateOpts{Name: "third", GatewayInfo: &gateway}).Extract()
	require.NoError(t, err)
	assert.Equal(t, "203.0.113.3", third.GatewayInfo.ExternalFixedIPs[0].IPAddress)

	enableSNAT := false
	for _, tc := range []struct {
		name    string
		gateway routers.GatewayInfo
		status  int
	}{
		{name: "missing network", gateway: routers.GatewayInfo{NetworkID: missingNetworkMarker}, status: http.StatusNotFound},
		{name: "private network", gateway: routers.GatewayInfo{NetworkID: resources.networkID}, status: http.StatusBadRequest},
		{
			name:    "administrator SNAT option",
			gateway: routers.GatewayInfo{NetworkID: public.ID, EnableSNAT: &enableSNAT},
			status:  http.StatusForbidden,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := routers.Update(ctx, client, third.ID, routers.UpdateOpts{GatewayInfo: &tc.gateway}).Extract()

			assert.True(t, gophercloud.ResponseCodeIs(err, tc.status), "%v", err)
		})
	}
	unchanged, err := routers.Get(ctx, client, third.ID).Extract()
	require.NoError(t, err)
	assert.Equal(t, third.GatewayInfo, unchanged.GatewayInfo)
}

func TestRouterExternalGatewayWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	admin := map[string]string{"X-SandStack-Admin-Token": adminToken}
	send := func(method, path, body string) (int, []byte) {
		t.Helper()
		status, headers, response := request(t, instance, method, path, body, auth)
		requireJSON(t, headers)
		return status, response
	}
	routerGateway := func(body []byte) map[string]json.RawMessage {
		t.Helper()
		var response struct {
			Router map[string]json.RawMessage `json:"router"`
		}
		require.NoError(t, json.Unmarshal(body, &response))
		_, hasPortID := response.Router["gw_port_id"]
		assert.False(t, hasPortID, "gw_port_id is not a router API attribute")
		return response.Router
	}

	// https://docs.openstack.org/api-ref/network/v2/#list-networks
	status, body := send(http.MethodGet, networksPath+"?router:external=true", "")

	require.Equal(t, http.StatusOK, status, "%s", body)
	assert.JSONEq(t, fmt.Sprintf(`{"networks":[{
		"id":%q,"name":"public","project_id":%q,"tenant_id":%q,"status":"ACTIVE",
		"admin_state_up":true,"shared":false,"router:external":true,"subnets":[%q],
		"port_security_enabled":true,"tags":[]
	}]}`, externalNetworkID, externalProjectID, externalProjectID, externalSubnetID), string(body))
	status, body = send(http.MethodGet, networksPath+"?router:external=maybe", "")
	assert.Equal(t, http.StatusBadRequest, status, "%s", body)
	assertPortNeutronError(t, body, "InvalidInput")
	status, body = send(http.MethodPut, networksPath+"/"+externalNetworkID, `{"network":{"name":"mine"}}`)
	assert.Equal(t, http.StatusForbidden, status, "%s", body)
	assertPortNeutronError(t, body, "PolicyNotAuthorized")
	status, body = send(http.MethodDelete, networksPath+"/"+externalNetworkID, "")
	assert.Equal(t, http.StatusForbidden, status, "%s", body)
	status, body = send(http.MethodPost, portsPath, fmt.Sprintf(`{"port":{"network_id":%q}}`, externalNetworkID))
	assert.Equal(t, http.StatusNotFound, status, "%s", body)

	// https://docs.openstack.org/api-ref/network/v2/#create-router
	status, body = send(http.MethodPost, routersPath,
		fmt.Sprintf(`{"router":{"name":"edge","external_gateway_info":{"network_id":%q}}}`, externalNetworkID))

	require.Equal(t, http.StatusCreated, status, "%s", body)
	created := routerGateway(body)
	assert.JSONEq(t, fmt.Sprintf(`{
		"network_id":%q,"enable_snat":true,
		"external_fixed_ips":[{"subnet_id":%q,"ip_address":"203.0.113.2"}]
	}`, externalNetworkID, externalSubnetID), string(created["external_gateway_info"]))
	var routerID string
	require.NoError(t, json.Unmarshal(created["id"], &routerID))
	path := routersPath + "/" + routerID

	for _, tc := range []struct {
		name   string
		body   string
		status int
		kind   string
	}{
		{name: "missing network ID", body: `{"router":{"external_gateway_info":{"other":1}}}`, status: http.StatusBadRequest, kind: "BadRequest"},
		{
			name:   "unknown network",
			body:   fmt.Sprintf(`{"router":{"external_gateway_info":{"network_id":%q}}}`, missingNetworkMarker),
			status: http.StatusNotFound, kind: "NetworkNotFound",
		},
		{
			name:   "private network",
			body:   fmt.Sprintf(`{"router":{"external_gateway_info":{"network_id":%q}}}`, fixtureNetworkID),
			status: http.StatusBadRequest, kind: "BadRequest",
		},
		{
			name: "SNAT policy",
			body: fmt.Sprintf(`{"router":{"external_gateway_info":{"network_id":%q,"enable_snat":true}}}`,
				externalNetworkID),
			status: http.StatusForbidden, kind: "PolicyNotAuthorized",
		},
		{
			name: "fixed IP policy",
			body: fmt.Sprintf(`{"router":{"external_gateway_info":{"network_id":%q,"external_fixed_ips":[]}}}`,
				externalNetworkID),
			status: http.StatusForbidden, kind: "PolicyNotAuthorized",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, body := send(http.MethodPut, path, tc.body)

			assert.Equal(t, tc.status, status, "%s", body)
			assertPortNeutronError(t, body, tc.kind)
		})
	}
	status, body = send(http.MethodGet, path, "")
	require.Equal(t, http.StatusOK, status, "%s", body)
	assert.Equal(t, created, routerGateway(body))

	// https://docs.openstack.org/api-ref/network/v2/#update-router
	upstreamRoute := `{"router":{"routes":[{"destination":"198.51.100.0/24","nexthop":"203.0.113.1"}]}}`
	status, body = send(http.MethodPut, path, upstreamRoute)
	require.Equal(t, http.StatusOK, status, "%s", body)
	status, body = send(http.MethodPut, path, `{"router":{"external_gateway_info":null}}`)
	assert.Equal(t, http.StatusBadRequest, status, "%s", body)
	assertPortNeutronError(t, body, "InvalidRoutes")
	status, body = send(http.MethodPut, path, `{"router":{"routes":[],"external_gateway_info":null}}`)

	require.Equal(t, http.StatusOK, status, "%s", body)
	cleared := routerGateway(body)
	assert.JSONEq(t, "null", string(cleared["external_gateway_info"]))
	assert.JSONEq(t, "[]", string(cleared["routes"]))

	status, body = send(http.MethodPost, networksPath, `{"network":{"name":"overlap"}}`)
	require.Equal(t, http.StatusCreated, status, "%s", body)
	var overlap struct {
		Network wireNetwork `json:"network"`
	}
	require.NoError(t, json.Unmarshal(body, &overlap))
	status, body = send(http.MethodPost, subnetsPath, fmt.Sprintf(
		`{"subnet":{"network_id":%q,"cidr":"203.0.113.0/28","ip_version":4}}`, overlap.Network.ID))
	require.Equal(t, http.StatusCreated, status, "%s", body)
	var subnet struct {
		Subnet subnets.Subnet `json:"subnet"`
	}
	require.NoError(t, json.Unmarshal(body, &subnet))
	status, body = send(http.MethodPut, path+"/add_router_interface", fmt.Sprintf(`{"subnet_id":%q}`, subnet.Subnet.ID))
	require.Equal(t, http.StatusOK, status, "%s", body)
	status, body = send(http.MethodPut, path,
		fmt.Sprintf(`{"router":{"external_gateway_info":{"network_id":%q}}}`, externalNetworkID))
	assert.Equal(t, http.StatusBadRequest, status, "%s", body)
	status, body = send(http.MethodPut, path+"/remove_router_interface", fmt.Sprintf(`{"subnet_id":%q}`, subnet.Subnet.ID))
	require.Equal(t, http.StatusOK, status, "%s", body)

	status, body = send(http.MethodPut, path,
		fmt.Sprintf(`{"router":{"external_gateway_info":{"network_id":%q}}}`, externalNetworkID))
	require.Equal(t, http.StatusOK, status, "%s", body)
	status, body = send(http.MethodPut, path, `{"router":{"external_gateway_info":{}}}`)
	require.Equal(t, http.StatusOK, status, "%s", body)
	assert.JSONEq(t, "null", string(routerGateway(body)["external_gateway_info"]))

	status, body = send(http.MethodPut, path,
		fmt.Sprintf(`{"router":{"external_gateway_info":{"network_id":%q}}}`, externalNetworkID))
	require.Equal(t, http.StatusOK, status, "%s", body)
	status, _, body = request(t, instance, http.MethodDelete, path, "", auth)
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	status, _, body = request(t, instance, http.MethodPost, "/_sandstack/reset", "", admin)
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	auth["X-Auth-Token"] = authenticate(t, instance).Token()
	status, body = send(http.MethodGet, networksPath+"/"+externalNetworkID, "")
	assert.Equal(t, http.StatusOK, status, "%s", body)
}
