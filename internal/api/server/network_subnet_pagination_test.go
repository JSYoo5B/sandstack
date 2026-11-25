package server_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/subnets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const missingSubnetMarker = "00000000-0000-0000-0000-000000000000"

func TestGophercloudPaginatesFilteredSubnets(t *testing.T) {
	instance := startServer(t, "")
	client := subnetListClient(t, authenticate(t, instance))
	fixture := createSubnetPagingFixtures(t, client)
	list := func(opts subnets.ListOpts) []subnets.Subnet {
		t.Helper()
		pages, err := subnets.List(client, opts).AllPages(t.Context())
		require.NoError(t, err)
		result, err := subnets.ExtractSubnets(pages)
		require.NoError(t, err)
		return result
	}
	withDHCP := true
	opts := subnets.ListOpts{EnableDHCP: &withDHCP, NetworkID: fixtureNetworkID, Limit: 2}
	want := []subnets.Subnet{fixture[0], fixture[2], fixture[3], fixture[4]}
	assert.Equal(t, want, list(opts))
	opts.Marker = fixture[1].ID
	assert.Equal(t, want[1:], list(opts), "an existing marker excluded by filters still positions the cursor")
	opts.Marker = fixture[4].ID
	assert.Empty(t, list(opts))
	opts.Marker = missingSubnetMarker
	assert.Equal(t, want, list(opts), "an unknown cursor restarts the listing")
}

func TestSubnetPaginationWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	client := subnetListClient(t, provider)
	fixture := createSubnetPagingFixtures(t, client)
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	query := url.Values{
		"name": {fixture[0].Name, fixture[2].Name, fixture[0].Name}, "network_id": {fixtureNetworkID},
		"limit": {"2"}, "page_reverse": {"TrUe"}, "marker": {missingSubnetMarker},
	}
	read := func(rawQuery string, want ...string) map[string]string {
		t.Helper()
		status, headers, body := request(t, instance, http.MethodGet, subnetsPath+"?"+rawQuery, "", auth)
		require.Equal(t, http.StatusOK, status, "%s", body)
		requireJSON(t, headers)
		var page struct {
			Subnets []wireSubnet       `json:"subnets"`
			Links   []gophercloud.Link `json:"subnets_links"`
		}
		require.NoError(t, json.Unmarshal(body, &page))
		require.NotNil(t, page.Subnets)
		ids := make([]string, 0, len(page.Subnets))
		for _, subnet := range page.Subnets {
			ids = append(ids, subnet.ID)
		}
		assert.Equal(t, append([]string{}, want...), ids)
		if len(page.Links) == 0 {
			var fields map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(body, &fields))
			assert.NotContains(t, fields, "subnets_links")
		}
		links := make(map[string]string, len(page.Links))
		for _, link := range page.Links {
			require.NotContains(t, links, link.Rel)
			links[link.Rel] = link.Href
		}
		return links
	}
	check := func(href, marker string, previous bool) *url.URL {
		t.Helper()
		require.NotEmpty(t, href)
		link, err := url.Parse(href)
		require.NoError(t, err)
		assert.True(t, link.IsAbs())
		assert.Equal(t, instance.URL+subnetsPath, link.Scheme+"://"+link.Host+link.Path)
		expected, err := url.ParseQuery(query.Encode())
		require.NoError(t, err)
		expected.Set("marker", marker)
		if previous {
			expected.Set("page_reverse", "True")
		} else {
			expected.Del("page_reverse")
		}
		assert.Equal(t, expected, link.Query())
		return link
	}
	a, b, c, d := fixture[0].ID, fixture[2].ID, fixture[3].ID, fixture[4].ID

	// https://docs.openstack.org/api-ref/network/v2/#pagination
	terminal := read(query.Encode(), c, d)
	require.Len(t, terminal, 1)
	previous := check(terminal["previous"], c, true)
	first := read(previous.RawQuery, a, b)
	require.Len(t, first, 1)
	next := check(first["next"], b, false)
	assert.Equal(t, terminal, read(next.RawQuery, c, d))

	query.Set("marker", fixture[1].ID)
	reverse := read(query.Encode(), a)
	require.Len(t, reverse, 1)
	next = check(reverse["next"], a, false)
	forward := read(next.RawQuery, b, c)
	require.Len(t, forward, 2)
	check(forward["previous"], b, true)
	check(forward["next"], c, false)
	query.Set("marker", a)
	assert.Empty(t, read(query.Encode()))
	query.Set("limit", "0")
	query.Set("marker", d)
	assert.Empty(t, read(query.Encode(), a, b, c, d))
	query.Del("limit")
	assert.Empty(t, read(query.Encode(), a, b, c, d))

	status, _, body := request(t, instance, http.MethodGet, subnetsPath+"?limit=2", "", nil)
	require.Equal(t, http.StatusUnauthorized, status, "%s", body)
	for _, invalid := range []string{
		"limit=-1", "limit=noninteger", "limit=1&limit=2",
		"limit=2&marker=%zz",
		"limit=2&marker=" + a + "&marker=" + b,
		"limit=2&page_reverse=true&page_reverse=false",
	} {
		status, headers, body := request(t, instance, http.MethodGet, subnetsPath+"?"+invalid, "", auth)
		require.Equal(t, http.StatusBadRequest, status, "%s: %s", invalid, body)
		requireJSON(t, headers)
		assertPortNeutronError(t, body, "BadRequest")
	}
}

func createSubnetPagingFixtures(t *testing.T, client *gophercloud.ServiceClient) []subnets.Subnet {
	t.Helper()
	ctx := t.Context()
	names := []string{"page + a", "out of filter", "page & b", "page + a", "page & b"}
	fixture := make([]subnets.Subnet, 0, len(names))
	for i := range names {
		subnet, err := subnets.Create(ctx, client, subnets.CreateOpts{
			NetworkID: fixtureNetworkID, CIDR: fmt.Sprintf("10.85.%d.0/24", i), IPVersion: gophercloud.IPv4,
		}).Extract()
		require.NoError(t, err)
		require.NotEmpty(t, subnet.ID)
		fixture = append(fixture, *subnet)
	}
	slices.SortFunc(fixture, func(a, b subnets.Subnet) int { return strings.Compare(a.ID, b.ID) })
	for i, name := range names {
		dhcp := i != 1
		subnet, err := subnets.Update(ctx, client, fixture[i].ID, subnets.UpdateOpts{
			Name: &name, EnableDHCP: &dhcp,
		}).Extract()
		require.NoError(t, err)
		fixture[i] = *subnet
	}
	return fixture
}
