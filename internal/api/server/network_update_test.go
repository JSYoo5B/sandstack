package server_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/networks"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/subnets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGophercloudUpdatesNetworkIndependentlyOfOutage(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	client, err := openstack.NewNetworkV2(provider, gophercloud.EndpointOpts{
		Region: "RegionOne", Availability: gophercloud.AvailabilityPublic,
	})
	require.NoError(t, err)
	ctx := t.Context()
	initial, err := networks.Create(ctx, client, networks.CreateOpts{Name: "mutable-network"}).Extract()
	require.NoError(t, err)
	require.NotEmpty(t, initial.ID)
	subnet, err := subnets.Create(ctx, client, subnets.CreateOpts{
		NetworkID: initial.ID, CIDR: "10.79.0.0/24", IPVersion: gophercloud.IPv4,
	}).Extract()
	require.NoError(t, err)
	want := *initial
	want.Subnets = []string{subnet.ID}
	lookup := func() *networks.Network {
		t.Helper()
		network, err := networks.Get(ctx, client, initial.ID).Extract()
		require.NoError(t, err)
		return network
	}
	update := func(opts networks.UpdateOpts) *networks.Network {
		t.Helper()
		network, err := networks.Update(ctx, client, initial.ID, opts).Extract()
		require.NoError(t, err)
		return network
	}
	list := func(name, status string) []networks.Network {
		t.Helper()
		pages, err := networks.List(client, networks.ListOpts{Name: name, Status: status}).AllPages(ctx)
		require.NoError(t, err)
		networks, err := networks.ExtractNetworks(pages)
		require.NoError(t, err)
		return networks
	}
	fault := func(method string) {
		t.Helper()
		status, _, body := request(t, instance, method, "/_sandstack/network/networks/"+initial.ID+"/fault", "",
			map[string]string{"X-SandStack-Admin-Token": adminToken})
		require.Equal(t, http.StatusNoContent, status, "%s", body)
	}
	require.True(t, initial.AdminStateUp)
	require.Equal(t, "ACTIVE", initial.Status)
	assert.Equal(t, &want, lookup())

	fault(http.MethodPut)
	want.Status = "DOWN"
	assert.Equal(t, &want, lookup())
	renamed := "renamed-network"
	want.Name = renamed
	assert.Equal(t, &want, update(networks.UpdateOpts{Name: &renamed}))
	assert.Equal(t, []networks.Network{want}, list(renamed, "DOWN"))
	assert.Empty(t, list(initial.Name, "DOWN"))
	assert.Empty(t, list(renamed, "ACTIVE"))

	disabled := false
	enabled := true
	want.AdminStateUp = false
	assert.Equal(t, &want, update(networks.UpdateOpts{AdminStateUp: &disabled}))
	want.AdminStateUp = true
	assert.Equal(t, &want, update(networks.UpdateOpts{AdminStateUp: &enabled}))
	assert.Equal(t, &want, lookup())
	want.AdminStateUp = false
	assert.Equal(t, &want, update(networks.UpdateOpts{AdminStateUp: &disabled}))

	fault(http.MethodDelete)
	assert.Equal(t, &want, lookup())
	assert.Equal(t, []networks.Network{want}, list(renamed, "DOWN"))
	want.Status = "ACTIVE"
	want.AdminStateUp = true
	assert.Equal(t, &want, update(networks.UpdateOpts{AdminStateUp: &enabled}))
	assert.Empty(t, list(renamed, "DOWN"))
	assert.Equal(t, []networks.Network{want}, list(renamed, "ACTIVE"))

	empty := ""
	want.Name = ""
	assert.Equal(t, &want, update(networks.UpdateOpts{Name: &empty}))
	assert.Equal(t, &want, lookup())
	assert.Empty(t, list(renamed, "ACTIVE"))
}

func TestNetworkUpdateWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	path := networksPath + "/" + fixtureNetworkID
	read := func(body []byte) wireNetwork {
		t.Helper()
		var response struct {
			Network wireNetwork `json:"network"`
		}
		require.NoError(t, json.Unmarshal(body, &response))
		require.Equal(t, fixtureNetworkID, response.Network.ID)
		require.NotNil(t, response.Network.AdminStateUp)
		return response.Network
	}
	status, headers, body := request(t, instance, http.MethodGet, path, "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	want := read(body)
	const payload = `{"network":{"name":"wire-update","admin_state_up":false}}`

	// https://docs.openstack.org/api-ref/network/v2/#update-network
	status, _, body = request(t, instance, http.MethodPut, path, payload, nil)
	require.Equal(t, http.StatusUnauthorized, status, "%s", body)
	status, headers, body = request(t, instance, http.MethodPut, path, payload, auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	want.Name = "wire-update"
	want.Status = "DOWN"
	disabled := false
	want.AdminStateUp = &disabled
	assert.Equal(t, want, read(body))
	expected := string(body)

	status, headers, body = request(t, instance, http.MethodPut, path, `{"network":{}}`, auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	assert.JSONEq(t, expected, string(body))
	for _, invalid := range []string{
		`{}`,
		`{"network":[]}`,
		`{"network":{"name":null}}`,
		`{"network":{"admin_state_up":null}}`,
		`{"network":{"admin_state_up":"false"}}`,
		`{"network":{"name":"one","name":"two"}}`,
		`{"network":{"status":"ACTIVE","name":"must not change"}}`,
		`{"network":{"description":"unsupported"}}`,
		`{"network":{"subnets":[]}}`,
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
	status, headers, body = request(t, instance, http.MethodPut, networksPath+"/"+missing, payload, auth)
	require.Equal(t, http.StatusNotFound, status, "%s", body)
	requireJSON(t, headers)
	assertPortNeutronError(t, body, "NetworkNotFound")
}
