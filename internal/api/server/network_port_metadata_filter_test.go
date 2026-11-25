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

func TestGophercloudFiltersPortsByIDNameAndAdministrativeState(t *testing.T) {
	instance := startServer(t, "")
	resources := createRouterInterfaceResources(t, authenticate(t, instance))
	ctx := t.Context()
	client := resources.service
	const name = "matched-port"
	disabled := false
	selected, err := ports.Create(ctx, client, ports.CreateOpts{
		NetworkID: resources.networkID, Name: name, AdminStateUp: &disabled,
	}).Extract()
	require.NoError(t, err)
	require.Len(t, selected.FixedIPs, 1)
	enabled := true
	decoy, err := ports.Create(ctx, client, ports.CreateOpts{
		NetworkID: resources.networkID, Name: name, AdminStateUp: &enabled, FixedIPs: []ports.IP{},
	}).Extract()
	require.NoError(t, err)
	list := func(opts ports.ListOpts) []ports.Port {
		t.Helper()
		pages, err := ports.List(client, opts).AllPages(ctx)
		require.NoError(t, err)
		items, err := ports.ExtractPorts(pages)
		require.NoError(t, err)
		return items
	}
	byID := ports.ListOpts{ID: selected.ID}
	byName := ports.ListOpts{Name: name}
	byNameAndState := ports.ListOpts{Name: name, AdminStateUp: &disabled}
	assert.Equal(t, []ports.Port{*selected}, list(byID))
	assert.ElementsMatch(t, []ports.Port{*selected, *decoy}, list(byName))
	assert.Equal(t, []ports.Port{*selected}, list(byNameAndState))
	assert.Empty(t, list(ports.ListOpts{ID: decoy.ID, Name: name, AdminStateUp: &disabled}))

	matched := ports.ListOpts{
		ID: selected.ID, Name: name, AdminStateUp: &disabled, NetworkID: resources.networkID,
		DeviceID: resources.routerID, DeviceOwner: routerInterfaceOwner,
		FixedIPs: []ports.FixedIPOpts{{IPAddress: selected.FixedIPs[0].IPAddress, SubnetID: resources.subnetID}},
	}
	assert.Empty(t, list(matched))
	_, err = routers.AddInterface(ctx, client, resources.routerID, routers.AddInterfaceOpts{
		PortID: selected.ID,
	}).Extract()
	require.NoError(t, err)
	bound, err := ports.Get(ctx, client, selected.ID).Extract()
	require.NoError(t, err)
	require.Equal(t, routerInterfaceOwner, bound.DeviceOwner)
	assert.Equal(t, []ports.Port{*bound}, list(matched))

	renamed := "updated-router-port"
	revised, err := ports.Update(ctx, client, selected.ID, ports.UpdateOpts{
		Name: &renamed, AdminStateUp: &enabled,
	}).Extract()
	require.NoError(t, err)
	assert.Empty(t, list(matched))
	assert.Empty(t, list(byNameAndState))
	assert.Equal(t, []ports.Port{*decoy}, list(byName))
	matched.Name, matched.AdminStateUp = renamed, &enabled
	assert.Equal(t, []ports.Port{*revised}, list(matched))
	assert.Equal(t, []ports.Port{*revised}, list(byID))
}

func TestPortMetadataFiltersWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	resources := createRouterInterfaceResources(t, provider)
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	create := func(name string, adminUp bool) string {
		t.Helper()
		port, err := ports.Create(t.Context(), resources.service, ports.CreateOpts{
			NetworkID: resources.networkID, Name: name, AdminStateUp: &adminUp, FixedIPs: []ports.IP{},
		}).Extract()
		require.NoError(t, err)
		require.NotEmpty(t, port.ID)
		return port.ID
	}
	const sameName = "matching + port"
	disabled := create(sameName, false)
	enabled := create(sameName, true)
	unnamed := create("", false)

	// https://docs.openstack.org/api-ref/network/v2/#list-ports
	// https://docs.openstack.org/api-ref/network/v2/#filtering-and-column-selection
	for _, test := range []struct {
		name  string
		query url.Values
		want  []string
	}{
		{"repeated IDs", url.Values{"id": {disabled, enabled, disabled}}, []string{disabled, enabled}},
		{"repeated names and false", url.Values{"name": {sameName, ""}, "admin_state_up": {"off"}},
			[]string{disabled, unnamed}},
		{"repeated admin states", url.Values{"name": {sameName}, "admin_state_up": {"yes", "false"}},
			[]string{disabled, enabled}},
		{"empty name", url.Values{"name": {""}}, []string{unnamed}},
		{"all keys must match", url.Values{"id": {disabled, enabled}, "name": {sameName}, "admin_state_up": {"0"}},
			[]string{disabled}},
		{"malformed decoded ID", url.Values{"id": {"invalid + id"}}, nil},
		{"incompatible fields", url.Values{"id": {unnamed}, "name": {sameName}}, nil},
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
				require.NotNil(t, port.AdminStateUp)
				assert.Equal(t, port.ID == enabled, *port.AdminStateUp)
			}
			assert.ElementsMatch(t, test.want, ids)
		})
	}
	for _, query := range []string{"admin_state_up=", "admin_state_up=invalid", "admin_state_up=false&admin_state_up=invalid"} {
		status, headers, body := request(t, instance, http.MethodGet, portsPath+"?"+query, "", auth)
		require.Equal(t, http.StatusBadRequest, status, "%s: %s", query, body)
		requireJSON(t, headers)
		assertPortNeutronError(t, body, "InvalidInput")
	}
}
