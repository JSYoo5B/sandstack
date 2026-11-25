package server_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/subnets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGophercloudManagesSubnetHostRoutes(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	client, err := openstack.NewNetworkV2(provider, gophercloud.EndpointOpts{
		Region: "RegionOne", Availability: gophercloud.AvailabilityPublic,
	})
	require.NoError(t, err)
	ctx := t.Context()
	defaultRoute := subnets.HostRoute{DestinationCIDR: "0.0.0.0/0", NextHop: "192.0.2.1"}
	firstRoute := subnets.HostRoute{DestinationCIDR: "203.0.113.0/24", NextHop: "10.69.0.3"}
	initial := []subnets.HostRoute{defaultRoute, firstRoute}

	created, err := subnets.Create(ctx, client, subnets.CreateOpts{
		Name: "routes-subnet", NetworkID: fixtureNetworkID, CIDR: "10.69.0.0/24",
		IPVersion: gophercloud.IPv4, HostRoutes: initial,
	}).Extract()

	require.NoError(t, err)
	require.NotEmpty(t, created.ID)
	assert.ElementsMatch(t, initial, created.HostRoutes)
	check := func(want subnets.Subnet, got *subnets.Subnet) {
		t.Helper()
		require.NotNil(t, got)
		require.NotNil(t, got.HostRoutes)
		assert.ElementsMatch(t, want.HostRoutes, got.HostRoutes)
		actual := *got
		actual.HostRoutes = want.HostRoutes
		assert.Equal(t, want, actual)
	}
	read := func(want subnets.Subnet) {
		t.Helper()
		detail, err := subnets.Get(ctx, client, created.ID).Extract()
		require.NoError(t, err)
		check(want, detail)
		pages, err := subnets.List(client, nil).AllPages(ctx)
		require.NoError(t, err)
		listed, err := subnets.ExtractSubnets(pages)
		require.NoError(t, err)
		require.Len(t, listed, 1)
		check(want, &listed[0])
	}
	read(*created)

	replacement := []subnets.HostRoute{
		firstRoute, {DestinationCIDR: firstRoute.DestinationCIDR, NextHop: "198.51.100.9"},
	}
	updated, err := subnets.Update(ctx, client, created.ID, subnets.UpdateOpts{HostRoutes: &replacement}).Extract()

	require.NoError(t, err)
	want := *created
	want.HostRoutes = replacement
	check(want, updated)
	read(want)

	renamed := "same-host-routes"
	updated, err = subnets.Update(ctx, client, created.ID, subnets.UpdateOpts{Name: &renamed}).Extract()
	require.NoError(t, err)
	want.Name = renamed
	check(want, updated)
	read(want)

	empty := []subnets.HostRoute{}
	updated, err = subnets.Update(ctx, client, created.ID, subnets.UpdateOpts{HostRoutes: &empty}).Extract()

	require.NoError(t, err)
	want.HostRoutes = empty
	check(want, updated)
	read(want)
}

func TestSubnetHostRoutesWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	const cidr = "10.69.0.0/24"
	const initial = `[{"destination":"0.0.0.0/0","nexthop":"192.0.2.1"}]`
	payload := fmt.Sprintf(`{"subnet":{"network_id":%q,"ip_version":4,"cidr":%q,"host_routes":%s}}`,
		fixtureNetworkID, cidr, initial)
	read := func(body []byte) wireSubnet {
		t.Helper()
		var response struct {
			Subnet wireSubnet `json:"subnet"`
		}
		require.NoError(t, json.Unmarshal(body, &response))
		require.NotEmpty(t, response.Subnet.ID)
		require.NotEmpty(t, response.Subnet.HostRoutes)
		return response.Subnet
	}

	// https://docs.openstack.org/api-ref/network/v2/#create-subnet
	status, headers, body := request(t, instance, http.MethodPost, subnetsPath, payload, auth)
	require.Equal(t, http.StatusCreated, status, "%s", body)
	requireJSON(t, headers)
	created := read(body)
	assert.Equal(t, fixtureNetworkID, created.NetworkID)
	assert.Equal(t, cidr, created.CIDR)
	assert.JSONEq(t, initial, string(created.HostRoutes))
	path := subnetsPath + "/" + created.ID
	routeRequests := make([]map[string]string, 0, 21)
	for i := range 21 {
		routeRequests = append(routeRequests, map[string]string{
			"destination": fmt.Sprintf("203.0.113.%d/32", i+1),
			"nexthop":     "10.69.0.2",
		})
	}
	overflow, err := json.Marshal(routeRequests)
	require.NoError(t, err)

	// https://docs.openstack.org/api-ref/network/v2/#update-subnet
	for _, invalid := range []struct {
		routes string
		kind   string
	}{
		{`[{"destination":"203.0.113.10/24","nexthop":"192.0.2.1"}]`, "HTTPBadRequest"},
		{`[{"destination":"203.0.113.10","nexthop":"192.0.2.1"}]`, "HTTPBadRequest"},
		{`[{"destination":"127.0.0.0/8","nexthop":"192.0.2.1"}]`, "HTTPBadRequest"},
		{`[{"destination":"2001:db8::/64","nexthop":"192.0.2.1"}]`, "InvalidInput"},
		{`[{"destination":"203.0.113.0/24","nexthop":"2001:db8::1"}]`, "InvalidInput"},
		{`[{"destination":"203.0.113.0/24","nexthop":"192.0.2.999"}]`, "HTTPBadRequest"},
		{`[{"destination":"203.0.113.0/24","nexthop":"192.0.2.1","name":"extra"}]`, "HTTPBadRequest"},
		{`[{"destination":"203.0.113.0/24"}]`, "HTTPBadRequest"},
		{`{"destination":"203.0.113.0/24","nexthop":"192.0.2.1"}`, "HTTPBadRequest"},
		{`[{"destination":"0.0.0.0/0","nexthop":"192.0.2.1"},{"destination":"0.0.0.0/0","nexthop":"192.0.2.1"}]`,
			"HTTPBadRequest"},
		{string(overflow), "HostRoutesExhausted"},
	} {
		payload := `{"subnet":{"name":"must-not-change","host_routes":` + invalid.routes + `}}`
		status, headers, body := request(t, instance, http.MethodPut, path, payload, auth)
		require.Equal(t, http.StatusBadRequest, status, "%s: %s", invalid.routes, body)
		requireJSON(t, headers)
		assertPortNeutronError(t, body, invalid.kind)
	}
	status, headers, body = request(t, instance, http.MethodGet, path, "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	assert.Equal(t, created, read(body))

	atLimit := routeRequests[:20]
	encoded, err := json.Marshal(atLimit)
	require.NoError(t, err)
	status, headers, body = request(t, instance, http.MethodPut, path, `{"subnet":{"host_routes":`+string(encoded)+`}}`, auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	var actual []map[string]string
	require.NoError(t, json.Unmarshal(read(body).HostRoutes, &actual))
	assert.ElementsMatch(t, atLimit, actual)

	// Neutron's subnet schema converts null to an empty list.
	status, headers, body = request(t, instance, http.MethodPut, path, `{"subnet":{"host_routes":null}}`, auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	cleared := read(body)
	assert.Equal(t, created.ID, cleared.ID)
	assert.JSONEq(t, "[]", string(cleared.HostRoutes))
}
