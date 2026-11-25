package server_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/ports"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGophercloudOrdersPortPagesByNameAndID(t *testing.T) {
	instance := startServer(t, "")
	client, fixture := createPortPagingFixtures(t, authenticate(t, instance))
	ctx := t.Context()
	// Give the excluded marker the same name as two visible ports and an ID between them.
	tiedName := fixture[0].Name
	marker, err := ports.Update(ctx, client, fixture[1].ID, ports.UpdateOpts{Name: &tiedName}).Extract()
	require.NoError(t, err)
	fixture[1] = *marker
	list := func(opts ports.ListOpts) []ports.Port {
		t.Helper()
		pages, err := ports.List(client, opts).AllPages(ctx)
		require.NoError(t, err)
		result, err := ports.ExtractPorts(pages)
		require.NoError(t, err)
		return result
	}
	opts := ports.ListOpts{NetworkID: fixtureNetworkID, Limit: 1}
	assert.Equal(t, fixture, list(opts))
	opts.SortKey, opts.SortDir = "name", "asc"
	assert.Equal(t, []ports.Port{fixture[2], fixture[4], fixture[0], fixture[1], fixture[3]}, list(opts))
	opts.SortDir = "desc"
	assert.Equal(t, []ports.Port{fixture[0], fixture[1], fixture[3], fixture[2], fixture[4]}, list(opts))

	enabled := true
	opts.Marker, opts.AdminStateUp = marker.ID, &enabled
	assert.Equal(t, []ports.Port{fixture[3], fixture[2], fixture[4]}, list(opts))
	opts.SortDir = "asc"
	assert.Equal(t, []ports.Port{fixture[3]}, list(opts))

	movedName := "keep (middle)"
	_, err = ports.Update(ctx, client, marker.ID, ports.UpdateOpts{Name: &movedName}).Extract()
	require.NoError(t, err)
	assert.Equal(t, []ports.Port{fixture[0], fixture[3]}, list(opts))
	opts.SortDir = "desc"
	assert.Equal(t, []ports.Port{fixture[2], fixture[4]}, list(opts))
}

func TestPortNameOrderWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	_, fixture := createPortPagingFixtures(t, provider)
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	read := func(query string, want ...string) []portPageLink {
		t.Helper()
		status, headers, body := request(t, instance, http.MethodGet, portsPath+"?"+query, "", auth)
		require.Equal(t, http.StatusOK, status, "%s", body)
		requireJSON(t, headers)
		var page struct {
			Ports []wirePort     `json:"ports"`
			Links []portPageLink `json:"ports_links"`
		}
		require.NoError(t, json.Unmarshal(body, &page))
		ids := make([]string, 0, len(page.Ports))
		for _, port := range page.Ports {
			ids = append(ids, port.ID)
		}
		assert.Equal(t, want, ids)
		return page.Links
	}

	// https://docs.openstack.org/api-ref/network/v2/#sorting
	assert.Empty(t, read("sort_key=&sort_dir=", fixture[0].ID, fixture[1].ID, fixture[2].ID, fixture[3].ID, fixture[4].ID))
	query := url.Values{
		"sort_key": {"name"}, "sort_dir": {"desc"}, "limit": {"1"},
		"name": {fixture[0].Name, fixture[2].Name}, "admin_state_up": {"true"},
	}
	links := read(query.Encode(), fixture[0].ID)
	require.Len(t, links, 1)
	assert.Equal(t, "next", links[0].Rel)
	next, err := url.Parse(links[0].Href)
	require.NoError(t, err)
	assert.Equal(t, instance.URL+portsPath, next.Scheme+"://"+next.Host+next.Path)
	query.Set("marker", fixture[0].ID)
	assert.Equal(t, query, next.Query())
	links = read(next.RawQuery, fixture[3].ID)
	require.Len(t, links, 2)
	assert.ElementsMatch(t, []string{"previous", "next"}, []string{links[0].Rel, links[1].Rel})

	for _, invalid := range []string{
		"sort_key=name", "sort_dir=asc", "sort_key=name&sort_dir=",
		"sort_key=Name&sort_dir=asc", "sort_key=name&sort_dir=ASC",
		"sort_key=id&sort_dir=asc", "sort_key=name&sort_dir=random",
		"sort_key=name&sort_key=name&sort_dir=asc",
		"sort_key=name&sort_dir=asc&sort_dir=desc",
	} {
		status, headers, body := request(t, instance, http.MethodGet, portsPath+"?"+invalid, "", auth)
		require.Equal(t, http.StatusBadRequest, status, "%s: %s", invalid, body)
		requireJSON(t, headers)
		assertPortNeutronError(t, body, "BadRequest")
	}
}
