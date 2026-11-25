package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/networks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apinetwork "github.com/JSYoo5B/sandstack/internal/api/network"
	appnetwork "github.com/JSYoo5B/sandstack/internal/app/network"
	domainnetwork "github.com/JSYoo5B/sandstack/internal/domain/network"
)

const (
	networkPagingName    = "page + network"
	missingNetworkMarker = "00000000-0000-0000-0000-000000000000"
)

func TestGophercloudPaginatesFilteredNetworks(t *testing.T) {
	instance := startServer(t, "")
	client := networkPagingClient(t, authenticate(t, instance))
	fixture := createNetworkPagingFixtures(t, client)
	list := func(opts networks.ListOpts) []networks.Network {
		t.Helper()
		pages, err := networks.List(client, opts).AllPages(t.Context())
		require.NoError(t, err)
		result, err := networks.ExtractNetworks(pages)
		require.NoError(t, err)
		return result
	}
	adminUp := true
	opts := networks.ListOpts{Name: networkPagingName, Status: "ACTIVE", AdminStateUp: &adminUp, Limit: 2}
	want := []networks.Network{fixture[0], fixture[2], fixture[3], fixture[4]}
	assert.Equal(t, want, list(opts))
	opts.Marker = fixture[1].ID
	assert.Equal(t, want[1:], list(opts), "a visible marker can be excluded by list filters")
	opts.Marker = fixture[4].ID
	assert.Empty(t, list(opts))
	opts.Marker = missingNetworkMarker
	_, err := networks.List(client, opts).AllPages(t.Context())
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusNotFound), "%v", err)
}

func TestNetworkPaginationWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	fixture := createNetworkPagingFixtures(t, networkPagingClient(t, provider))
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	query := url.Values{
		"name": {networkPagingName, "not & present"}, "status": {"ACTIVE"}, "admin_state_up": {"true"},
		"limit": {"2"}, "page_reverse": {"True"},
	}
	read := func(rawQuery string, want ...string) map[string]string {
		t.Helper()
		status, headers, body := request(t, instance, http.MethodGet, networksPath+"?"+rawQuery, "", auth)
		require.Equal(t, http.StatusOK, status, "%s", body)
		requireJSON(t, headers)
		var page struct {
			Networks []wireNetwork      `json:"networks"`
			Links    []gophercloud.Link `json:"networks_links"`
		}
		require.NoError(t, json.Unmarshal(body, &page))
		require.NotNil(t, page.Networks)
		ids := make([]string, 0, len(page.Networks))
		for _, network := range page.Networks {
			ids = append(ids, network.ID)
		}
		assert.Equal(t, append([]string{}, want...), ids)
		if len(page.Links) == 0 {
			var fields map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(body, &fields))
			assert.NotContains(t, fields, "networks_links")
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
		assert.Equal(t, instance.URL+networksPath, link.Scheme+"://"+link.Host+link.Path)
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
	check(reverse["next"], a, false)
	query.Set("marker", a)
	assert.Empty(t, read(query.Encode()))
	query.Set("marker", missingNetworkMarker)
	query.Set("limit", "0")
	assert.Empty(t, read(query.Encode(), a, b, c, d))
	query.Del("limit")
	assert.Empty(t, read(query.Encode(), a, b, c, d))

	status, _, body := request(t, instance, http.MethodGet, networksPath+"?limit=1", "", nil)
	assert.Equal(t, http.StatusUnauthorized, status, "%s", body)
	status, headers, body := request(t, instance, http.MethodGet,
		networksPath+"?limit=1&marker="+missingNetworkMarker, "", auth)
	require.Equal(t, http.StatusNotFound, status, "%s", body)
	requireJSON(t, headers)
	assertPortNeutronError(t, body, "NetworkNotFound")
	for _, invalid := range []string{
		"limit=-1", "limit=noninteger", "limit=1&limit=2", "marker=%zz", "marker=a&marker=b",
		"page_reverse=true&page_reverse=false", "limit=1&sort_key=name",
	} {
		status, headers, body := request(t, instance, http.MethodGet, networksPath+"?"+invalid, "", auth)
		require.Equal(t, http.StatusBadRequest, status, "%s: %s", invalid, body)
		requireJSON(t, headers)
		assertPortNeutronError(t, body, "BadRequest")
	}
}

func TestNetworkPaginationHidesForeignMarker(t *testing.T) {
	const foreignID = "10000000-0000-4000-8000-000000000001"
	const visibleID = "20000000-0000-4000-8000-000000000002"
	service := appnetwork.New(domainnetwork.Network{
		ID: foreignID, ProjectID: "foreign",
	}, func() string { return visibleID })
	_, err := service.Create("caller", "visible network", false)
	require.NoError(t, err)
	router := chi.NewRouter()
	apinetwork.Register(router, service,
		func(string) (string, error) { return "caller", nil },
		func(_ *http.Request, path string) string { return "http://neutron.test" + path },
	)
	read := func(limit string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, networksPath+"?limit="+limit+"&marker="+foreignID, nil)
		r.Header.Set("X-Auth-Token", "fixture-token")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}

	marked := read("1")
	require.Equal(t, http.StatusNotFound, marked.Code, "%s", marked.Body)
	assertPortNeutronError(t, marked.Body.Bytes(), "NetworkNotFound")
	unpaged := read("0")
	require.Equal(t, http.StatusOK, unpaged.Code, "%s", unpaged.Body)
	var response struct {
		Networks []wireNetwork `json:"networks"`
	}
	require.NoError(t, json.Unmarshal(unpaged.Body.Bytes(), &response))
	require.Len(t, response.Networks, 1)
	assert.Equal(t, visibleID, response.Networks[0].ID)
}

func networkPagingClient(t *testing.T, provider *gophercloud.ProviderClient) *gophercloud.ServiceClient {
	t.Helper()
	client, err := openstack.NewNetworkV2(provider, gophercloud.EndpointOpts{
		Region: "RegionOne", Availability: gophercloud.AvailabilityPublic,
	})
	require.NoError(t, err)
	return client
}

func createNetworkPagingFixtures(t *testing.T, client *gophercloud.ServiceClient) []networks.Network {
	t.Helper()
	ctx := t.Context()
	fixture := make([]networks.Network, 5)
	for i := range fixture {
		network, err := networks.Create(ctx, client, networks.CreateOpts{Name: networkPagingName}).Extract()
		require.NoError(t, err)
		fixture[i] = *network
	}
	slices.SortFunc(fixture, func(a, b networks.Network) int { return strings.Compare(a.ID, b.ID) })
	name, adminUp := "out of filter", false
	marker, err := networks.Update(ctx, client, fixture[1].ID, networks.UpdateOpts{
		Name: &name, AdminStateUp: &adminUp,
	}).Extract()
	require.NoError(t, err)
	fixture[1] = *marker
	return fixture
}
