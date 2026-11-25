package server_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/layer3/routers"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/ports"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGophercloudReplacesAndClearsRouterRoutes(t *testing.T) {
	instance := startServer(t, "")
	resources := createRouterInterfaceResources(t, authenticate(t, instance))
	ctx := t.Context()
	service := resources.service
	routerID := resources.routerID
	interfaceInfo, err := routers.AddInterface(ctx, service, routerID, routers.AddInterfaceOpts{
		SubnetID: resources.subnetID,
	}).Extract()
	require.NoError(t, err)
	require.NotEmpty(t, interfaceInfo.PortID)
	get := func() *routers.Router {
		t.Helper()
		detail, err := routers.Get(ctx, service, routerID).Extract()
		require.NoError(t, err)
		return detail
	}
	assert.Equal(t, []routers.Route{}, get().Routes)

	first := []routers.Route{{DestinationCIDR: "192.0.2.0/24", NextHop: "10.66.0.2"}}
	updated, err := routers.Update(ctx, service, routerID, routers.UpdateOpts{Routes: &first}).Extract()
	require.NoError(t, err)
	assert.Equal(t, first, updated.Routes)
	replacement := []routers.Route{
		{DestinationCIDR: "198.51.100.0/24", NextHop: "10.66.0.3"},
		{DestinationCIDR: "203.0.113.0/24", NextHop: "10.66.0.2"},
	}
	updated, err = routers.Update(ctx, service, routerID, routers.UpdateOpts{Routes: &replacement}).Extract()
	require.NoError(t, err)
	assert.ElementsMatch(t, replacement, updated.Routes)

	updated, err = routers.Update(ctx, service, routerID, routers.UpdateOpts{Name: "routes-retained"}).Extract()
	require.NoError(t, err)
	assert.ElementsMatch(t, replacement, updated.Routes)
	assert.Equal(t, updated, get())
	pages, err := routers.List(service, nil).AllPages(ctx)
	require.NoError(t, err)
	listed, err := routers.ExtractRouters(pages)
	require.NoError(t, err)
	assert.Equal(t, []routers.Router{*updated}, listed)

	for _, nextHop := range []string{"10.67.0.2", "10.66.0.1"} {
		rejected := []routers.Route{{DestinationCIDR: "192.0.2.0/24", NextHop: nextHop}}
		_, err := routers.Update(ctx, service, routerID, routers.UpdateOpts{
			Name: "must-not-change", Routes: &rejected,
		}).Extract()
		assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusBadRequest), "route via %s: %v", nextHop, err)
		assert.Equal(t, updated, get())
	}

	byPort := routers.RemoveInterfaceOpts{PortID: interfaceInfo.PortID}
	bySubnet := routers.RemoveInterfaceOpts{SubnetID: resources.subnetID}
	for _, selector := range []routers.RemoveInterfaceOpts{byPort, bySubnet} {
		_, err := routers.RemoveInterface(ctx, service, routerID, selector).Extract()
		assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusConflict), "remove %+v: %v", selector, err)
		preserved, err := ports.Get(ctx, service, interfaceInfo.PortID).Extract()
		require.NoError(t, err)
		assert.Equal(t, routerID, preserved.DeviceID)
		assert.Equal(t, updated, get())
	}

	empty := []routers.Route{}
	cleared, err := routers.Update(ctx, service, routerID, routers.UpdateOpts{Routes: &empty}).Extract()
	require.NoError(t, err)
	assert.Equal(t, empty, cleared.Routes)
	assert.Equal(t, cleared, get())
	removed, err := routers.RemoveInterface(ctx, service, routerID, byPort).Extract()
	require.NoError(t, err)
	assert.Equal(t, interfaceInfo, removed)
}

func TestRouterRoutesWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	resources := createRouterInterfaceResources(t, provider)
	_, err := routers.AddInterface(t.Context(), resources.service, resources.routerID, routers.AddInterfaceOpts{
		SubnetID: resources.subnetID,
	}).Extract()
	require.NoError(t, err)
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	path := routersPath + "/" + resources.routerID
	readRoutes := func(body []byte) json.RawMessage {
		t.Helper()
		var response struct {
			Router wireRouter `json:"router"`
		}
		require.NoError(t, json.Unmarshal(body, &response))
		require.Equal(t, resources.routerID, response.Router.ID)
		require.NotEmpty(t, response.Router.Routes)
		return response.Router.Routes
	}
	const route = `{"destination":"192.0.2.0/24","nexthop":"10.66.0.2"}`
	expected := "[" + route + "]"
	const prefix = `{"router":{"routes":`
	const suffix = `}}`

	// https://docs.openstack.org/api-ref/network/v2/#update-router
	status, headers, body := request(t, instance, http.MethodPut, path, prefix+expected+suffix, auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	assert.JSONEq(t, expected, string(readRoutes(body)))

	for _, invalid := range []struct {
		name   string
		routes string
		kind   string
	}{
		{"host bits in destination", `[{"destination":"192.0.2.1/24","nexthop":"10.66.0.2"}]`, "BadRequest"},
		{"missing next hop", `[{"destination":"192.0.2.0/24"}]`, "BadRequest"},
		{"duplicate next hop field", `[{"destination":"192.0.2.0/24","nexthop":"10.66.0.2","nexthop":"10.66.0.3"}]`,
			"BadRequest"},
		{"duplicate route", "[" + route + "," + route + "]", "InvalidRoutes"},
	} {
		t.Run(invalid.name, func(t *testing.T) {
			status, headers, body := request(t, instance, http.MethodPut, path, prefix+invalid.routes+suffix, auth)
			require.Equal(t, http.StatusBadRequest, status, "%s", body)
			requireJSON(t, headers)
			assertPortNeutronError(t, body, invalid.kind)
		})
	}
	status, headers, body = request(t, instance, http.MethodGet, path, "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	assert.JSONEq(t, expected, string(readRoutes(body)))

	// Neutron converts null to an empty list in neutron-lib's extraroute definition.
	status, headers, body = request(t, instance, http.MethodPut, path, prefix+"null"+suffix, auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	assert.JSONEq(t, "[]", string(readRoutes(body)))
}
