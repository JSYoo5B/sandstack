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
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/layer3/routers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const missingRouterMarker = "00000000-0000-0000-0000-000000000000"

func TestGophercloudPaginatesFilteredRouters(t *testing.T) {
	instance := startServer(t, "")
	client, fixture := createRouterPagingFixtures(t, authenticate(t, instance))
	list := func(opts routers.ListOpts) []routers.Router {
		t.Helper()
		pages, err := routers.List(client, opts).AllPages(t.Context())
		require.NoError(t, err)
		result, err := routers.ExtractRouters(pages)
		require.NoError(t, err)
		return result
	}
	down := false
	opts := routers.ListOpts{AdminStateUp: &down, Limit: 1}
	want := []routers.Router{fixture[0], fixture[2], fixture[3], fixture[4]}
	assert.Equal(t, want, list(opts))
	opts.Marker = fixture[1].ID
	assert.Equal(t, want[1:], list(opts))
	opts.Name = fixture[2].Name
	assert.Equal(t, []routers.Router{fixture[2], fixture[4]}, list(opts))
	opts.Marker = fixture[4].ID
	assert.Empty(t, list(opts))
	opts.Marker = missingRouterMarker
	_, err := routers.List(client, opts).AllPages(t.Context())
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusNotFound), "unknown marker: %v", err)
	opts.Limit = 0
	assert.Equal(t, []routers.Router{fixture[2], fixture[4]}, list(opts))
}

func TestRouterPaginationWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	_, fixture := createRouterPagingFixtures(t, provider)
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	query := url.Values{
		"name": {fixture[0].Name, fixture[2].Name, fixture[0].Name}, "admin_state_up": {"false"},
		"limit": {"2"}, "page_reverse": {"TrUE"},
	}
	read := func(rawQuery string, want ...string) map[string]string {
		t.Helper()
		status, headers, body := request(t, instance, http.MethodGet, routersPath+"?"+rawQuery, "", auth)
		require.Equal(t, http.StatusOK, status, "%s", body)
		requireJSON(t, headers)
		var page struct {
			Routers []wireRouter       `json:"routers"`
			Links   []gophercloud.Link `json:"routers_links"`
		}
		require.NoError(t, json.Unmarshal(body, &page))
		require.NotNil(t, page.Routers)
		ids := make([]string, 0, len(page.Routers))
		for _, router := range page.Routers {
			ids = append(ids, router.ID)
			require.NotNil(t, router.AdminStateUp)
			assert.False(t, *router.AdminStateUp)
			assert.Equal(t, "ACTIVE", router.Status)
		}
		assert.Equal(t, append([]string{}, want...), ids)
		if len(page.Links) == 0 {
			var fields map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(body, &fields))
			assert.NotContains(t, fields, "routers_links")
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
		assert.Equal(t, instance.URL+routersPath, link.Scheme+"://"+link.Host+link.Path)
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
	middle := read(next.RawQuery, b, c)
	require.Len(t, middle, 2)
	check(middle["previous"], b, true)
	check(middle["next"], c, false)
	query.Set("marker", a)
	assert.Empty(t, read(query.Encode()))

	query.Set("marker", missingRouterMarker)
	status, headers, body := request(t, instance, http.MethodGet, routersPath+"?"+query.Encode(), "", auth)
	require.Equal(t, http.StatusNotFound, status, "%s", body)
	requireJSON(t, headers)
	assertPortNeutronError(t, body, "RouterNotFound")
	query.Set("limit", "0")
	assert.Empty(t, read(query.Encode(), a, b, c, d))
	query.Del("limit")
	assert.Empty(t, read(query.Encode(), a, b, c, d))

	status, _, body = request(t, instance, http.MethodGet, routersPath+"?limit=2", "", nil)
	require.Equal(t, http.StatusUnauthorized, status, "%s", body)
	for _, invalid := range []string{
		"limit=-1", "limit=bad", "limit=1&limit=2",
		"limit=2&marker=%zz", "limit=2&marker=" + a + "&marker=" + b,
		"limit=2&page_reverse=true&page_reverse=false",
	} {
		status, headers, body := request(t, instance, http.MethodGet, routersPath+"?"+invalid, "", auth)
		require.Equal(t, http.StatusBadRequest, status, "%s: %s", invalid, body)
		requireJSON(t, headers)
		assertPortNeutronError(t, body, "BadRequest")
	}
}

func createRouterPagingFixtures(
	t *testing.T, provider *gophercloud.ProviderClient,
) (*gophercloud.ServiceClient, []routers.Router) {
	t.Helper()
	client, err := openstack.NewNetworkV2(provider, gophercloud.EndpointOpts{
		Region: "RegionOne", Availability: gophercloud.AvailabilityPublic,
	})
	require.NoError(t, err)
	ctx := t.Context()
	names := []string{"page + a", "page + a", "page & b", "page + a", "page & b"}
	fixture := make([]routers.Router, 0, len(names))
	for range names {
		router, err := routers.Create(ctx, client, routers.CreateOpts{}).Extract()
		require.NoError(t, err)
		require.NotEmpty(t, router.ID)
		fixture = append(fixture, *router)
	}
	slices.SortFunc(fixture, func(a, b routers.Router) int { return strings.Compare(a.ID, b.ID) })
	for i, name := range names {
		enabled := i == 1
		router, err := routers.Update(ctx, client, fixture[i].ID, routers.UpdateOpts{
			Name: name, AdminStateUp: &enabled,
		}).Extract()
		require.NoError(t, err)
		fixture[i] = *router
	}
	return client, fixture
}
