package server_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/ports"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGophercloudPaginatesFilteredPorts(t *testing.T) {
	instance := startServer(t, "")
	client, fixture := createPortPagingFixtures(t, authenticate(t, instance))
	list := func(opts ports.ListOpts) []ports.Port {
		t.Helper()
		pages, err := ports.List(client, opts).AllPages(t.Context())
		require.NoError(t, err)
		items, err := ports.ExtractPorts(pages)
		require.NoError(t, err)
		return items
	}
	up := true
	opts := ports.ListOpts{NetworkID: fixtureNetworkID, AdminStateUp: &up, Limit: 2}
	want := []ports.Port{fixture[0], fixture[2], fixture[3], fixture[4]}
	assert.Equal(t, want, list(opts))

	opts.Marker = fixture[1].ID
	assert.Equal(t, want[1:], list(opts), "an existing port can mark a page even when excluded by the filters")
	opts.Marker = fixture[4].ID
	assert.Empty(t, list(opts))
	opts.Limit = 0
	assert.Equal(t, want, list(opts))
}

func TestPortForwardPaginationWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	_, fixture := createPortPagingFixtures(t, provider)
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	query := url.Values{
		"network_id": {fixtureNetworkID},
		"name":       {fixture[0].Name, fixture[2].Name, fixture[0].Name},
		"limit":      {"2"},
		"marker":     {fixture[1].ID},
	}
	read := func(rawQuery string, want ...string) []portPageLink {
		t.Helper()
		status, headers, body := request(t, instance, http.MethodGet, portsPath+"?"+rawQuery, "", auth)
		require.Equal(t, http.StatusOK, status, "%s", body)
		requireJSON(t, headers)
		var page struct {
			Ports []wirePort     `json:"ports"`
			Links []portPageLink `json:"ports_links"`
		}
		require.NoError(t, json.Unmarshal(body, &page))
		require.NotNil(t, page.Ports)
		finished := make([]string, 0, len(page.Ports))
		for _, port := range page.Ports {
			finished = append(finished, port.ID)
		}
		assert.Equal(t, append([]string{}, want...), finished)
		if len(page.Links) == 0 {
			var fields map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(body, &fields))
			assert.NotContains(t, fields, "ports_links")
		}
		return page.Links
	}
	checkNext := func(links []portPageLink, marker string) *url.URL {
		t.Helper()
		require.Len(t, links, 1)
		assert.Equal(t, "next", links[0].Rel)
		next, err := url.Parse(links[0].Href)
		require.NoError(t, err)
		assert.True(t, next.IsAbs())
		assert.Equal(t, instance.URL+portsPath, next.Scheme+"://"+next.Host+next.Path)
		expected, err := url.ParseQuery(query.Encode())
		require.NoError(t, err)
		expected.Set("marker", marker)
		assert.Equal(t, expected, next.Query())
		return next
	}

	// https://docs.openstack.org/api-ref/network/v2/#pagination
	// https://github.com/openstack/neutron/blob/master/neutron/api/api_common.py
	links := read(query.Encode(), fixture[2].ID, fixture[3].ID)
	next := checkNext(links, fixture[3].ID)
	assert.Empty(t, read(next.RawQuery, fixture[4].ID))

	query.Del("marker")
	links = read(query.Encode(), fixture[0].ID, fixture[2].ID)
	next = checkNext(links, fixture[2].ID)
	assert.Empty(t, read(next.RawQuery, fixture[3].ID, fixture[4].ID), "a full terminal page has no link")
	query.Set("marker", "")
	links = read(query.Encode(), fixture[0].ID, fixture[2].ID)
	checkNext(links, fixture[2].ID)
	query.Set("marker", fixture[4].ID)
	assert.Empty(t, read(query.Encode()), "a marker at the end produces an empty page")

	const missing = "00000000-0000-0000-0000-000000000000"
	for _, marker := range []string{missing, "invalid + id"} {
		query.Set("marker", marker)
		status, headers, body := request(t, instance, http.MethodGet, portsPath+"?"+query.Encode(), "", auth)
		require.Equal(t, http.StatusNotFound, status, "%s", body)
		requireJSON(t, headers)
		assertPortNeutronError(t, body, "PortNotFound")
	}
	query.Set("limit", "0")
	all := []string{fixture[0].ID, fixture[2].ID, fixture[3].ID, fixture[4].ID}
	assert.Empty(t, read(query.Encode(), all...))
	query.Del("limit")
	assert.Empty(t, read(query.Encode(), all...))

	for _, bad := range []string{
		"limit=-1", "limit=noninteger", "limit=1&limit=2", "limit=2&marker=%zz",
		"limit=2&marker=" + fixture[0].ID + "&marker=" + fixture[2].ID,
		"limit=2&page_reverse=true",
	} {
		status, headers, body := request(t, instance, http.MethodGet, portsPath+"?"+bad, "", auth)
		require.Equal(t, http.StatusBadRequest, status, "%s: %s", bad, body)
		requireJSON(t, headers)
		assertPortNeutronError(t, body, "BadRequest")
	}
}

type portPageLink struct {
	Rel  string `json:"rel"`
	Href string `json:"href"`
}

func createPortPagingFixtures(
	t *testing.T, provider *gophercloud.ProviderClient,
) (*gophercloud.ServiceClient, []ports.Port) {
	t.Helper()
	client, err := openstack.NewNetworkV2(provider, gophercloud.EndpointOpts{
		Region: "RegionOne", Availability: gophercloud.AvailabilityPublic,
	})
	require.NoError(t, err)
	ctx := t.Context()
	names := []string{"keep + a", "out of filter", "keep & b", "keep + a", "keep & b"}
	fixture := make([]ports.Port, 0, len(names))
	for range names {
		port, err := ports.Create(ctx, client, ports.CreateOpts{
			NetworkID: fixtureNetworkID, FixedIPs: []ports.IP{},
		}).Extract()
		require.NoError(t, err)
		require.NotEmpty(t, port.ID)
		fixture = append(fixture, *port)
	}
	slices.SortFunc(fixture, func(a, b ports.Port) int { return strings.Compare(a.ID, b.ID) })
	for i, name := range names {
		up := i != 1
		port, err := ports.Update(ctx, client, fixture[i].ID, ports.UpdateOpts{
			Name: &name, AdminStateUp: &up,
		}).Extract()
		require.NoError(t, err)
		fixture[i] = *port
	}
	return client, fixture
}
