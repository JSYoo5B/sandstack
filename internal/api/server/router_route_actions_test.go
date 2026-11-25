package server_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/layer3/extraroutes"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/layer3/routers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGophercloudAddsAndRemovesRouterRoutePairs(t *testing.T) {
	instance := startServer(t, "")
	resources := prepareRouterRouteActions(t, authenticate(t, instance))
	service := resources.service
	routerID := resources.routerID
	ctx := t.Context()
	first := routers.Route{DestinationCIDR: "192.0.2.0/24", NextHop: "10.66.0.2"}
	sameDestination := routers.Route{DestinationCIDR: first.DestinationCIDR, NextHop: "10.66.0.3"}
	overlappingDestination := routers.Route{DestinationCIDR: "192.0.2.0/25", NextHop: "10.66.0.4"}
	baseline := []routers.Route{first}
	_, err := routers.Update(ctx, service, routerID, routers.UpdateOpts{Routes: &baseline}).Extract()
	require.NoError(t, err)

	union := []routers.Route{first, sameDestination, overlappingDestination}
	add := []routers.Route{first, sameDestination, overlappingDestination, sameDestination}
	added, err := extraroutes.Add(ctx, service, routerID, extraroutes.Opts{Routes: &add}).Extract()

	require.NoError(t, err)
	assert.Equal(t, routerID, added.ID)
	assert.ElementsMatch(t, union, added.Routes)
	added, err = extraroutes.Add(ctx, service, routerID, extraroutes.Opts{Routes: &add}).Extract()
	require.NoError(t, err)
	assert.ElementsMatch(t, union, added.Routes)

	invalidBatch := []routers.Route{
		{DestinationCIDR: "198.51.100.0/24", NextHop: "10.66.0.5"},
		{DestinationCIDR: "203.0.113.0/24", NextHop: "10.67.0.5"},
	}
	_, err = extraroutes.Add(ctx, service, routerID, extraroutes.Opts{Routes: &invalidBatch}).Extract()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusBadRequest), "add unreachable next hop: %v", err)
	afterFailure, err := routers.Get(ctx, service, routerID).Extract()
	require.NoError(t, err)
	assert.Equal(t, added, afterFailure)

	absentPair := routers.Route{DestinationCIDR: sameDestination.DestinationCIDR, NextHop: "10.66.0.6"}
	remove := []routers.Route{first, absentPair}
	remaining := []routers.Route{sameDestination, overlappingDestination}
	removed, err := extraroutes.Remove(ctx, service, routerID, extraroutes.Opts{Routes: &remove}).Extract()

	require.NoError(t, err)
	assert.Equal(t, routerID, removed.ID)
	assert.ElementsMatch(t, remaining, removed.Routes)
	removed, err = extraroutes.Remove(ctx, service, routerID, extraroutes.Opts{Routes: &remove}).Extract()
	require.NoError(t, err)
	assert.ElementsMatch(t, remaining, removed.Routes)
	detail, err := routers.Get(ctx, service, routerID).Extract()
	require.NoError(t, err)
	assert.Equal(t, removed, detail)
}

func TestConcurrentGophercloudAddsPreserveRouterRoutes(t *testing.T) {
	instance := startServer(t, "")
	resources := prepareRouterRouteActions(t, authenticate(t, instance))
	ctx := t.Context()
	service := resources.service
	routerID := resources.routerID
	const requests = 8
	baseline := []routers.Route{{DestinationCIDR: "203.0.113.0/24", NextHop: "10.66.0.2"}}
	_, err := routers.Update(ctx, service, routerID, routers.UpdateOpts{Routes: &baseline}).Extract()
	require.NoError(t, err)
	want := append([]routers.Route{}, baseline...)
	start := make(chan struct{})
	results := make(chan error, requests)
	for i := range requests {
		route := routers.Route{DestinationCIDR: fmt.Sprintf("192.0.2.%d/32", i+1), NextHop: "10.66.0.2"}
		want = append(want, route)
		go func() {
			<-start
			routes := []routers.Route{route}
			_, err := extraroutes.Add(ctx, service, routerID, extraroutes.Opts{Routes: &routes}).Extract()
			results <- err
		}()
	}

	close(start)
	for range requests {
		assert.NoError(t, <-results)
	}
	detail, err := routers.Get(ctx, service, routerID).Extract()
	require.NoError(t, err)
	assert.ElementsMatch(t, want, detail.Routes)
}

func TestRouterRouteActionsWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	resources := prepareRouterRouteActions(t, provider)
	projectID := routerProjectID(t, provider)
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	path := routersPath + "/" + resources.routerID
	add := path + "/add_extraroutes"
	remove := path + "/remove_extraroutes"
	readRouter := func(body []byte) wireRouter {
		t.Helper()
		var response struct {
			Router wireRouter `json:"router"`
		}
		require.NoError(t, json.Unmarshal(body, &response))
		require.Equal(t, resources.routerID, response.Router.ID)
		require.NotEmpty(t, response.Router.Routes)
		return response.Router
	}
	checkRouter := func(want wireRouter, body []byte) {
		t.Helper()
		got := readRouter(body)
		assert.JSONEq(t, string(want.Routes), string(got.Routes))
		got.Routes = want.Routes
		assert.Equal(t, want, got)
	}
	status, headers, body := request(t, instance, http.MethodGet, path, "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	baseline := readRouter(body)
	assert.Equal(t, projectID, baseline.ProjectID)
	assert.Equal(t, projectID, baseline.TenantID)
	assert.JSONEq(t, "[]", string(baseline.Routes))
	const routes = `[{"destination":"192.0.2.0/24","nexthop":"10.66.0.2"}]`
	const payload = `{"router":{"routes":` + routes + `}}`
	const noOp = `{"router":{"routes":[]}}`

	// https://docs.openstack.org/api-ref/network/v2/#add-extra-routes-to-router
	status, _, body = request(t, instance, http.MethodPut, add, payload, nil)
	require.Equal(t, http.StatusUnauthorized, status, "%s", body)
	status, headers, body = request(t, instance, http.MethodPut, add, payload, auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	want := baseline
	want.Routes = json.RawMessage(routes)
	checkRouter(want, body)

	for _, action := range []string{add, remove} {
		status, headers, body = request(t, instance, http.MethodPut, action, noOp, auth)
		require.Equal(t, http.StatusOK, status, "%s", body)
		requireJSON(t, headers)
		checkRouter(want, body)
		for _, bad := range []string{
			`{"router":{}}`,
			`{"router":{"routes":null}}`,
			`{"router":{"routes":{"destination":"192.0.2.0/24"}}}`,
			`{"router":{"routes":[{"destination":"192.0.2.0/24"}]}}`,
			`{"router":{"routes":[],"name":"must not change"}}`,
		} {
			status, headers, body = request(t, instance, http.MethodPut, action, bad, auth)
			require.Equal(t, http.StatusBadRequest, status, "%s %s: %s", action, bad, body)
			requireJSON(t, headers)
			assertPortNeutronError(t, body, "BadRequest")
		}
	}
	status, headers, body = request(t, instance, http.MethodGet, path, "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	checkRouter(want, body)

	// https://docs.openstack.org/api-ref/network/v2/#remove-extra-routes-from-router
	status, headers, body = request(t, instance, http.MethodPut, remove, payload, auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	checkRouter(baseline, body)
}

func prepareRouterRouteActions(t *testing.T, provider *gophercloud.ProviderClient) routerInterfaceResources {
	t.Helper()
	resources := createRouterInterfaceResources(t, provider)
	_, err := routers.AddInterface(t.Context(), resources.service, resources.routerID, routers.AddInterfaceOpts{
		SubnetID: resources.subnetID,
	}).Extract()
	require.NoError(t, err)
	return resources
}
