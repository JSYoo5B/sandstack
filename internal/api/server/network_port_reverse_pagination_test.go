package server_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGophercloudFollowsPreviousAndNextPortPageLinks(t *testing.T) {
	instance := startServer(t, "")
	client, fixture := createPortPagingFixtures(t, authenticate(t, instance))
	baseURL := client.ServiceURL("ports")
	baseQuery := url.Values{
		"name": {fixture[0].Name, fixture[2].Name}, "network_id": {fixtureNetworkID},
		"sort_key": {"name"}, "sort_dir": {"desc"}, "limit": {"2"},
	}
	query := func(limit, marker, reverse string) url.Values {
		t.Helper()
		result, err := url.ParseQuery(baseQuery.Encode())
		require.NoError(t, err)
		result.Set("limit", limit)
		if marker != "" {
			result.Set("marker", marker)
		}
		if reverse != "" {
			result.Set("page_reverse", reverse)
		}
		return result
	}
	follow := func(href string, want ...string) map[string]string {
		t.Helper()
		var page struct {
			Ports []wirePort     `json:"ports"`
			Links []portPageLink `json:"ports_links"`
		}
		response, err := client.Get(t.Context(), href, &page, nil)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, response.StatusCode)
		requireJSON(t, response.Header)
		require.NotNil(t, page.Ports)
		ids := make([]string, 0, len(page.Ports))
		for _, port := range page.Ports {
			ids = append(ids, port.ID)
		}
		assert.Equal(t, append([]string{}, want...), ids)
		links := make(map[string]string, len(page.Links))
		for _, link := range page.Links {
			require.NotContains(t, links, link.Rel)
			links[link.Rel] = link.Href
		}
		return links
	}
	check := func(href string, expected url.Values) {
		t.Helper()
		require.NotEmpty(t, href)
		link, err := url.Parse(href)
		require.NoError(t, err)
		assert.True(t, link.IsAbs())
		assert.Equal(t, baseURL, link.Scheme+"://"+link.Host+link.Path)
		assert.Equal(t, expected, link.Query())
	}
	a, b, c, d := fixture[0].ID, fixture[3].ID, fixture[2].ID, fixture[4].ID

	// https://docs.openstack.org/api-ref/network/v2/#pagination
	terminal := follow(baseURL+"?"+query("3", "", "TrUE").Encode(), b, c, d)
	require.Len(t, terminal, 1)
	check(terminal["previous"], query("3", b, "True"))
	first := follow(terminal["previous"], a)
	require.Len(t, first, 1)
	check(first["next"], query("3", a, ""))
	assert.Equal(t, terminal, follow(first["next"], b, c, d))

	middle := follow(baseURL+"?"+query("2", d, "true").Encode(), b, c)
	require.Len(t, middle, 2)
	check(middle["previous"], query("2", b, "True"))
	check(middle["next"], query("2", c, ""))
	first = follow(middle["previous"], a)
	require.Len(t, first, 1)
	terminal = follow(middle["next"], d)
	require.Len(t, terminal, 1)
	check(terminal["previous"], query("2", d, "True"))
	assert.Equal(t, middle, follow(terminal["previous"], b, c))
	assert.Empty(t, follow(baseURL+"?"+query("2", a, "true").Encode()))

	for _, nonTrue := range []string{"false", "1", "yes", "true "} {
		forward := follow(baseURL+"?"+query("2", "", nonTrue).Encode(), a, b)
		require.Len(t, forward, 1)
		check(forward["next"], query("2", b, ""))
	}
	assert.Empty(t, follow(baseURL+"?"+query("0", d, "true").Encode(), a, b, c, d))
}
