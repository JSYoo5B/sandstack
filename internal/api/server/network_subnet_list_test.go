package server_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/networks"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/subnets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGophercloudListsSubnetsByExactFilters(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	client := subnetListClient(t, provider)
	first, unnamed, other := createSubnetListFixtures(t, client)
	projectID := routerProjectID(t, provider)
	for _, subnet := range []*subnets.Subnet{first, unnamed, other} {
		require.Equal(t, projectID, subnet.ProjectID)
	}
	public, err := subnets.Get(t.Context(), client, externalSubnetID).Extract()
	require.NoError(t, err)
	disabled := false
	for _, test := range []struct {
		name string
		opts subnets.ListOptsBuilder
		want []subnets.Subnet
	}{
		{"unfiltered", nil, []subnets.Subnet{*first, *unnamed, *other, *public}},
		{"name", &subnets.ListOpts{Name: first.Name}, []subnets.Subnet{*first, *other}},
		{"network", &subnets.ListOpts{NetworkID: first.NetworkID}, []subnets.Subnet{*first, *unnamed}},
		{"cidr", &subnets.ListOpts{CIDR: first.CIDR}, []subnets.Subnet{*first, *other}},
		{"disabled dhcp", &subnets.ListOpts{EnableDHCP: &disabled}, []subnets.Subnet{*unnamed, *other, *public}},
		{"combined", &subnets.ListOpts{
			Name: other.Name, NetworkID: other.NetworkID, CIDR: other.CIDR, EnableDHCP: &disabled,
		}, []subnets.Subnet{*other}},
	} {
		t.Run(test.name, func(t *testing.T) {
			pages, err := subnets.List(client, test.opts).AllPages(t.Context())
			require.NoError(t, err)
			listed, err := subnets.ExtractSubnets(pages)
			require.NoError(t, err)
			assert.ElementsMatch(t, test.want, listed)
		})
	}

	unscoped := authOptions(instance)
	unscoped.Scope.ProjectName = "nonexistent-project"
	_, err = openstack.AuthenticatedClient(t.Context(), unscoped)
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusUnauthorized), "unavailable project: %v", err)
}

func TestSubnetListFiltersWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	first, unnamed, other := createSubnetListFixtures(t, subnetListClient(t, provider))
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	const unknownID = "00000000-0000-0000-0000-000000000000"

	// https://docs.openstack.org/api-ref/network/v2/#list-subnets
	// https://docs.openstack.org/api-ref/network/v2/#filtering-and-column-selection
	for _, test := range []struct {
		name  string
		query url.Values
		want  []string
	}{
		{
			name:  "repeated names include exact empty",
			query: url.Values{"name": {first.Name, "", first.Name}},
			want:  []string{first.ID, unnamed.ID, other.ID},
		},
		{
			name: "repeated CIDRs and network combined with DHCP false",
			query: url.Values{
				"cidr": {first.CIDR, unnamed.CIDR}, "network_id": {unknownID, first.NetworkID},
				"enable_dhcp": {"off"},
			},
			want: []string{unnamed.ID},
		},
		{
			name:  "repeated DHCP aliases broaden one field only",
			query: url.Values{"enable_dhcp": {"yes", "0"}, "network_id": {first.NetworkID}},
			want:  []string{first.ID, unnamed.ID},
		},
		{
			name:  "valid CIDR containing host bits is not masked for matching",
			query: url.Values{"cidr": {"10.77.0.9/24"}},
		},
		{
			name:  "CIDR is exact rather than containment",
			query: url.Values{"cidr": {"10.77.0.0/16"}},
		},
		{
			name:  "case-sensitive name",
			query: url.Values{"name": {"MATCHED-SUBNET"}},
		},
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
			var ids []string
			for _, subnet := range response.Subnets {
				ids = append(ids, subnet.ID)
			}
			assert.ElementsMatch(t, test.want, ids)
		})
	}
	status, _, body := request(t, instance, http.MethodGet, subnetsPath+"?"+url.Values{
		"cidr": {first.CIDR},
	}.Encode(), "", nil)
	assert.Equal(t, http.StatusUnauthorized, status, "%s", body)

	for _, bad := range []struct {
		query string
		kind  string
	}{
		{"cidr=invalid", "InvalidInput"},
		{"cidr=10.77.0.0", "InvalidInput"},
		{"cidr=", "InvalidInput"},
		{"enable_dhcp=false&enable_dhcp=maybe", "InvalidInput"},
		{"enable_dhcp=", "InvalidInput"},
		{"ip_version=4", "BadRequest"},
		{"sort_key=name", "BadRequest"},
	} {
		status, headers, body := request(t, instance, http.MethodGet, subnetsPath+"?"+bad.query, "", auth)
		require.Equal(t, http.StatusBadRequest, status, "%s: %s", bad.query, body)
		requireJSON(t, headers)
		assertPortNeutronError(t, body, bad.kind)
	}
}

func subnetListClient(t *testing.T, provider *gophercloud.ProviderClient) *gophercloud.ServiceClient {
	t.Helper()
	client, err := openstack.NewNetworkV2(provider, gophercloud.EndpointOpts{
		Region: "RegionOne", Availability: gophercloud.AvailabilityPublic,
	})
	require.NoError(t, err)
	return client
}

func createSubnetListFixtures(
	t *testing.T, client *gophercloud.ServiceClient,
) (first, unnamed, other *subnets.Subnet) {
	t.Helper()
	ctx := t.Context()
	createNetwork := func(name string) string {
		t.Helper()
		network, err := networks.Create(ctx, client, networks.CreateOpts{Name: name}).Extract()
		require.NoError(t, err)
		require.NotEmpty(t, network.ID)
		return network.ID
	}
	createSubnet := func(networkID, name, cidr string, dhcp bool) *subnets.Subnet {
		t.Helper()
		subnet, err := subnets.Create(ctx, client, subnets.CreateOpts{
			NetworkID: networkID, Name: name, CIDR: cidr, IPVersion: gophercloud.IPv4, EnableDHCP: &dhcp,
		}).Extract()
		require.NoError(t, err)
		require.NotEmpty(t, subnet.ID)
		return subnet
	}
	firstNetwork := createNetwork("subnet-filter-first")
	secondNetwork := createNetwork("subnet-filter-second")
	return createSubnet(firstNetwork, "matched-subnet", "10.77.0.0/24", true),
		createSubnet(firstNetwork, "", "10.78.0.0/24", false),
		createSubnet(secondNetwork, "matched-subnet", "10.77.0.0/24", false)
}
