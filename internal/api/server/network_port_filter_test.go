package server_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/layer3/routers"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/ports"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const routerInterfaceOwner = "network:router_interface"

func TestGophercloudDiscoversRouterPortsByDeviceFilters(t *testing.T) {
	instance := startServer(t, "")
	resources := createRouterInterfaceResources(t, authenticate(t, instance))
	ctx := t.Context()
	service := resources.service
	candidate, err := ports.Create(ctx, service, ports.CreateOpts{
		NetworkID: resources.networkID, Name: "interface-to-bind",
	}).Extract()
	require.NoError(t, err)
	require.NotEmpty(t, candidate.ID)
	unbound, err := ports.Create(ctx, service, ports.CreateOpts{
		NetworkID: resources.networkID, Name: "unbound-neighbor", FixedIPs: []ports.IP{},
	}).Extract()
	require.NoError(t, err)
	require.NotEmpty(t, unbound.ID)

	list := func(opts ports.ListOpts) []ports.Port {
		t.Helper()
		pages, err := ports.List(service, opts).AllPages(ctx)
		require.NoError(t, err)
		items, err := ports.ExtractPorts(pages)
		require.NoError(t, err)
		return items
	}
	byDevice := ports.ListOpts{DeviceID: resources.routerID}
	byOwner := ports.ListOpts{DeviceOwner: routerInterfaceOwner}
	byNetwork := ports.ListOpts{NetworkID: resources.networkID}
	allFilters := ports.ListOpts{
		DeviceID: resources.routerID, DeviceOwner: routerInterfaceOwner, NetworkID: resources.networkID,
	}
	assert.Empty(t, list(byDevice))
	assert.Empty(t, list(byOwner))
	assert.ElementsMatch(t, []ports.Port{*candidate, *unbound}, list(byNetwork))

	added, err := routers.AddInterface(ctx, service, resources.routerID, routers.AddInterfaceOpts{
		PortID: candidate.ID,
	}).Extract()

	require.NoError(t, err)
	require.Equal(t, candidate.ID, added.PortID)
	bound := *candidate
	bound.DeviceID = resources.routerID
	bound.DeviceOwner = routerInterfaceOwner
	assert.Equal(t, []ports.Port{bound}, list(byDevice))
	assert.Equal(t, []ports.Port{bound}, list(byOwner))
	assert.Equal(t, []ports.Port{bound}, list(allFilters))
	assert.ElementsMatch(t, []ports.Port{bound, *unbound}, list(byNetwork))
	allFilters.NetworkID = fixtureNetworkID
	assert.Empty(t, list(allFilters))

	_, err = routers.RemoveInterface(ctx, service, resources.routerID, routers.RemoveInterfaceOpts{
		PortID: candidate.ID,
	}).Extract()

	require.NoError(t, err)
	assert.Empty(t, list(byDevice))
	assert.Empty(t, list(byOwner))
	assert.Equal(t, []ports.Port{*unbound}, list(byNetwork))
}

func TestPortDeviceFilterWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	resources := createRouterInterfaceResources(t, provider)
	service := resources.service
	ctx := t.Context()
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	added, err := routers.AddInterface(ctx, service, resources.routerID, routers.AddInterfaceOpts{
		SubnetID: resources.subnetID,
	}).Extract()
	require.NoError(t, err)
	require.NotEmpty(t, added.PortID)
	createUnbound := func(networkID string) string {
		t.Helper()
		port, err := ports.Create(ctx, service, ports.CreateOpts{
			NetworkID: networkID, FixedIPs: []ports.IP{},
		}).Extract()
		require.NoError(t, err)
		require.NotEmpty(t, port.ID)
		return port.ID
	}
	unbound := createUnbound(resources.networkID)
	otherNetwork := createUnbound(fixtureNetworkID)
	const unknownID = "00000000-0000-0000-0000-000000000000"

	// https://docs.openstack.org/api-ref/network/v2/#filtering-and-column-selection
	// https://docs.openstack.org/api-ref/network/v2/#list-ports
	for _, test := range []struct {
		name  string
		query url.Values
		ids   []string
	}{
		{
			name:  "repeated device ids include blank and router",
			query: url.Values{"device_id": {"", resources.routerID, resources.routerID}},
			ids:   []string{added.PortID, unbound, otherNetwork},
		},
		{
			name:  "repeated owners include blank and router",
			query: url.Values{"device_owner": {routerInterfaceOwner, ""}},
			ids:   []string{added.PortID, unbound, otherNetwork},
		},
		{
			name: "combined same-field OR and cross-field AND",
			query: url.Values{
				"device_id": {"", resources.routerID}, "device_owner": {routerInterfaceOwner},
				"network_id": {unknownID, resources.networkID},
			},
			ids: []string{added.PortID},
		},
		{
			name:  "blank device id",
			query: url.Values{"device_id": {""}, "network_id": {resources.networkID}},
			ids:   []string{unbound},
		},
		{
			name:  "blank device owner",
			query: url.Values{"device_owner": {""}, "network_id": {fixtureNetworkID}},
			ids:   []string{otherNetwork},
		},
		{
			name:  "owner uses exact case-sensitive equality",
			query: url.Values{"device_owner": {"NETWORK:ROUTER_INTERFACE"}},
			ids:   []string{},
		},
		{
			name:  "known fields must all match",
			query: url.Values{"device_id": {resources.routerID}, "device_owner": {""}},
			ids:   []string{},
		},
		{
			name:  "unknown device id is a nonmatch",
			query: url.Values{"device_id": {unknownID}},
			ids:   []string{},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			status, headers, body := request(t, instance, http.MethodGet, portsPath+"?"+test.query.Encode(), "", auth)
			require.Equal(t, http.StatusOK, status, "%s", body)
			requireJSON(t, headers)
			var response struct {
				Ports []wirePort `json:"ports"`
			}
			require.NoError(t, json.Unmarshal(body, &response))
			require.NotNil(t, response.Ports)
			ids := make([]string, 0, len(response.Ports))
			for _, port := range response.Ports {
				ids = append(ids, port.ID)
			}
			assert.ElementsMatch(t, test.ids, ids)
		})
	}

	status, headers, body := request(t, instance, http.MethodGet,
		portsPath+"?device_id="+resources.routerID+"&unknown_filter=value", "", auth)
	require.Equal(t, http.StatusBadRequest, status, "%s", body)
	requireJSON(t, headers)
	assertPortNeutronError(t, body, "BadRequest")
}
