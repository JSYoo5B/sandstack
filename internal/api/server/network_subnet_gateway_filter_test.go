package server_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/subnets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGophercloudListsSubnetsByIDAndCurrentGateway(t *testing.T) {
	instance := startServer(t, "")
	client := subnetListClient(t, authenticate(t, instance))
	first, unnamed, other := createSubnetListFixtures(t, client)
	require.Equal(t, first.GatewayIP, other.GatewayIP)
	require.NotEqual(t, first.GatewayIP, unnamed.GatewayIP)
	ctx := t.Context()
	list := func(opts subnets.ListOpts) []subnets.Subnet {
		t.Helper()
		pages, err := subnets.List(client, opts).AllPages(ctx)
		require.NoError(t, err)
		result, err := subnets.ExtractSubnets(pages)
		require.NoError(t, err)
		return result
	}
	id := subnets.ListOpts{ID: first.ID}
	oldGateway := subnets.ListOpts{GatewayIP: first.GatewayIP}
	assert.Equal(t, []subnets.Subnet{*first}, list(id))
	assert.ElementsMatch(t, []subnets.Subnet{*first, *other}, list(oldGateway))
	assert.Equal(t, []subnets.Subnet{*first}, list(subnets.ListOpts{ID: first.ID, GatewayIP: first.GatewayIP}))
	assert.Empty(t, list(subnets.ListOpts{ID: first.ID, GatewayIP: unnamed.GatewayIP}))

	gateway := "192.0.2.8"
	updated, err := subnets.Update(ctx, client, first.ID, subnets.UpdateOpts{GatewayIP: &gateway}).Extract()
	require.NoError(t, err)
	require.Equal(t, gateway, updated.GatewayIP)
	assert.Equal(t, []subnets.Subnet{*updated}, list(id))
	assert.Equal(t, []subnets.Subnet{*other}, list(oldGateway))
	assert.Equal(t, []subnets.Subnet{*updated}, list(subnets.ListOpts{ID: first.ID, GatewayIP: gateway}))

	unset := ""
	cleared, err := subnets.Update(ctx, client, first.ID, subnets.UpdateOpts{GatewayIP: &unset}).Extract()
	require.NoError(t, err)
	require.Empty(t, cleared.GatewayIP)
	assert.Equal(t, []subnets.Subnet{*cleared}, list(id))
	assert.Empty(t, list(subnets.ListOpts{GatewayIP: gateway}))
	assert.Equal(t, []subnets.Subnet{*other}, list(oldGateway))
}

func TestSubnetIDAndGatewayFiltersWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	client := subnetListClient(t, provider)
	first, unnamed, third := createSubnetListFixtures(t, client)
	unset := ""
	_, err := subnets.Update(t.Context(), client, third.ID, subnets.UpdateOpts{GatewayIP: &unset}).Extract()
	require.NoError(t, err)
	auth := map[string]string{"X-Auth-Token": provider.Token()}

	// https://docs.openstack.org/api-ref/network/v2/#list-subnets
	// https://docs.openstack.org/api-ref/network/v2/#filtering-and-column-selection
	for _, test := range []struct {
		name  string
		query url.Values
		want  []string
	}{
		{"IDs use OR", url.Values{"id": {first.ID, unnamed.ID, first.ID}}, []string{first.ID, unnamed.ID}},
		{
			"gateways use OR without matching an unset gateway",
			url.Values{"gateway_ip": {first.GatewayIP, unnamed.GatewayIP, "null"}},
			[]string{first.ID, unnamed.ID},
		},
		{
			"ID and gateway use AND alongside the existing name filter",
			url.Values{
				"id": {first.ID, unnamed.ID, third.ID}, "gateway_ip": {first.GatewayIP, unnamed.GatewayIP}, "name": {first.Name},
			},
			[]string{first.ID},
		},
		{"mismatched ID and gateway", url.Values{"id": {first.ID}, "gateway_ip": {unnamed.GatewayIP}}, nil},
		{"invalid ID literals", url.Values{"id": {"bad + id", "null", ""}}, nil},
		{"invalid gateway literals", url.Values{"gateway_ip": {"not an ip", "10.77.0", "null", ""}}, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			status, headers, body := request(t, instance, http.MethodGet, subnetsPath+"?"+test.query.Encode(), "", auth)
			require.Equal(t, http.StatusOK, status, "%s", body)
			requireJSON(t, headers)
			var response struct {
				Subnets []wireSubnet `json:"subnets"`
			}
			require.NoError(t, json.Unmarshal(body, &response))
			require.NotNil(t, response.Subnets)
			ids := make([]string, 0, len(response.Subnets))
			for _, subnet := range response.Subnets {
				ids = append(ids, subnet.ID)
			}
			assert.ElementsMatch(t, test.want, ids)
		})
	}
	status, headers, body := request(t, instance, http.MethodGet,
		subnetsPath+"?gateway_ip="+first.GatewayIP+"&unknown_field=value", "", auth)
	require.Equal(t, http.StatusBadRequest, status, "%s", body)
	requireJSON(t, headers)
	assertPortNeutronError(t, body, "BadRequest")
}
