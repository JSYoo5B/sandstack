package server_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/networks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNetworkListFilterWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	create := func(body string) string {
		t.Helper()
		status, _, response := request(t, instance, http.MethodPost, networksPath, body, auth)
		require.Equal(t, http.StatusCreated, status, "%s", response)
		var created struct {
			Network wireNetwork `json:"network"`
		}
		require.NoError(t, json.Unmarshal(response, &created))
		require.NotEmpty(t, created.Network.ID)
		return created.Network.ID
	}
	active := create(`{"network":{"name":"blue + lab"}}`)
	disabled := create(`{"network":{"name":"blue + lab","admin_state_up":false}}`)
	red := create(`{"network":{"name":"red","admin_state_up":false}}`)
	unnamed := create(`{"network":{}}`)
	blueFilter := "name=" + url.QueryEscape("blue + lab")

	// https://docs.openstack.org/api-ref/network/v2/#filtering-and-column-selection
	for _, example := range []struct {
		name  string
		query string
		want  []string
	}{
		{name: "no filter", want: []string{fixtureNetworkID, active, disabled, red, unnamed}},
		{name: "encoded exact name", query: blueFilter, want: []string{active, disabled}},
		{name: "repeated names use OR", query: blueFilter + "&name=red", want: []string{active, disabled, red}},
		{
			name:  "repeated statuses use OR",
			query: "status=DOWN&status=ACTIVE",
			want:  []string{fixtureNetworkID, active, disabled, red, unnamed},
		},
		{name: "one matching status", query: "status=unknown&status=DOWN", want: []string{disabled, red}},
		{name: "name and status use AND", query: blueFilter + "&status=DOWN", want: []string{disabled}},
		{
			name:  "repeated admin states use OR within a name",
			query: blueFilter + "&admin_state_up=false&admin_state_up=yes&admin_state_up=false",
			want:  []string{active, disabled},
		},
		{
			name:  "administrative state combines with name and status",
			query: blueFilter + "&status=DOWN&admin_state_up=off",
			want:  []string{disabled},
		},
		{name: "incompatible admin state and status", query: "status=ACTIVE&admin_state_up=0"},
		{name: "empty name matches unnamed", query: "name=", want: []string{unnamed}},
		{name: "partial name", query: "name=blue"},
		{name: "missing name", query: "name=missing"},
		{name: "empty status", query: "status="},
		{name: "unknown status", query: "status=unknown"},
		{name: "status is case sensitive", query: "status=down"},
	} {
		t.Run(example.name, func(t *testing.T) {
			path := networksPath
			if example.query != "" {
				path += "?" + example.query
			}
			status, headers, body := request(t, instance, http.MethodGet, path, "", auth)

			require.Equal(t, http.StatusOK, status, "%s", body)
			requireJSON(t, headers)
			var response struct {
				Networks []wireNetwork `json:"networks"`
			}
			require.NoError(t, json.Unmarshal(body, &response))
			require.NotNil(t, response.Networks, "empty queries must return an array")
			ids := make([]string, len(response.Networks))
			for i, network := range response.Networks {
				ids[i] = network.ID
			}
			assert.ElementsMatch(t, example.want, ids)
		})
	}

	for _, query := range []string{"admin_state_up=maybe", "admin_state_up=", "admin_state_up=false&admin_state_up=maybe"} {
		status, headers, body := request(t, instance, http.MethodGet, networksPath+"?"+query, "", auth)
		require.Equal(t, http.StatusBadRequest, status, "%s: %s", query, body)
		requireJSON(t, headers)
		assertPortNeutronError(t, body, "InvalidInput")
	}
	for _, path := range []string{
		networksPath + "?name=%zz",
		networksPath + "?id=" + active,
		networksPath + "/" + active + "?status=ACTIVE",
	} {
		status, _, body := request(t, instance, http.MethodGet, path, "", auth)
		assert.Equal(t, http.StatusBadRequest, status, "%s: %s", path, body)
	}
}

func TestGophercloudFiltersNetworksAcrossOperationalFault(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	service, err := openstack.NewNetworkV2(provider, gophercloud.EndpointOpts{
		Region: "RegionOne", Availability: gophercloud.AvailabilityPublic,
	})
	require.NoError(t, err)
	readList := func(opts networks.ListOpts) []networks.Network {
		t.Helper()
		pages, err := networks.List(service, opts).AllPages(t.Context())
		require.NoError(t, err)
		result, err := networks.ExtractNetworks(pages)
		require.NoError(t, err)
		return result
	}
	const networkName = "sdk + filter"
	active, err := networks.Create(t.Context(), service, networks.CreateOpts{Name: networkName}).Extract()
	require.NoError(t, err)
	require.NotNil(t, active)
	adminUp := false
	disabled, err := networks.Create(t.Context(), service, networks.CreateOpts{
		Name: networkName, AdminStateUp: &adminUp,
	}).Extract()
	require.NoError(t, err)
	require.NotNil(t, disabled)
	require.NotEmpty(t, active.ID)
	require.NotEmpty(t, disabled.ID)
	require.NotEqual(t, active.ID, disabled.ID)
	assert.Equal(t, "ACTIVE", active.Status)
	assert.Equal(t, "DOWN", disabled.Status)

	// https://docs.openstack.org/api-ref/network/v2/#list-networks
	activeFilter := networks.ListOpts{Name: networkName, Status: "ACTIVE"}
	downFilter := networks.ListOpts{Name: networkName, Status: "DOWN"}
	administrativelyDown := downFilter
	administrativelyDown.AdminStateUp = &adminUp
	enabled := true
	faultedAndEnabled := downFilter
	faultedAndEnabled.AdminStateUp = &enabled
	assert.Equal(t, []networks.Network{*active}, readList(activeFilter))
	assert.Equal(t, []networks.Network{*disabled}, readList(downFilter))
	assert.Equal(t, []networks.Network{*disabled}, readList(administrativelyDown))
	assert.Empty(t, readList(faultedAndEnabled))
	assert.Empty(t, readList(networks.ListOpts{Name: "sdk + FILTER"}))

	path := "/_sandstack/network/networks/" + active.ID + "/fault"
	control := map[string]string{"X-SandStack-Admin-Token": adminToken}
	status, _, body := request(t, instance, http.MethodPut, path, "", control)
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	down := *active
	down.Status = "DOWN"
	assert.Empty(t, readList(activeFilter))
	assert.ElementsMatch(t, []networks.Network{down, *disabled}, readList(downFilter))
	assert.Equal(t, []networks.Network{*disabled}, readList(administrativelyDown))
	assert.Equal(t, []networks.Network{down}, readList(faultedAndEnabled))

	status, _, body = request(t, instance, http.MethodDelete, path, "", control)

	require.Equal(t, http.StatusNoContent, status, "%s", body)
	assert.Equal(t, []networks.Network{*active}, readList(activeFilter))
	assert.Equal(t, []networks.Network{*disabled}, readList(downFilter))
	assert.Equal(t, []networks.Network{*disabled}, readList(administrativelyDown))
	assert.Empty(t, readList(faultedAndEnabled))
}
