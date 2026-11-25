package server_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/servers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGophercloudFiltersNovaServerByCurrentDisplayName(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	compute, err := openstack.NewComputeV2(provider, gophercloud.EndpointOpts{
		Region: "RegionOne", Availability: gophercloud.AvailabilityPublic,
	})
	require.NoError(t, err)
	ctx := t.Context()
	list := func(opts servers.ListOpts) []servers.Server {
		t.Helper()
		pages, err := servers.List(compute, opts).AllPages(ctx)
		require.NoError(t, err)
		items, err := servers.ExtractServers(pages)
		require.NoError(t, err)
		return items
	}
	before, err := servers.Get(ctx, compute, fixtureServerID).Extract()
	require.NoError(t, err)
	require.Equal(t, "fixture-server", before.Name)
	oldName := servers.ListOpts{Name: "^fixture-server$"}
	assert.Equal(t, []servers.Server{*before}, list(oldName))
	assert.Empty(t, list(servers.ListOpts{Name: "^renamed-fixture$"}))

	const renamed = "renamed-fixture"
	_, err = servers.Update(ctx, compute, fixtureServerID, servers.UpdateOpts{Name: renamed}).Extract()
	require.NoError(t, err)
	current, err := servers.Get(ctx, compute, fixtureServerID).Extract()
	require.NoError(t, err)
	require.Equal(t, renamed, current.Name)
	assert.Empty(t, list(oldName))
	assert.Equal(t, []servers.Server{*current}, list(servers.ListOpts{Name: "^renamed-fixture$"}))
	assert.Equal(t, []servers.Server{*current}, list(servers.ListOpts{Name: "fixture"}))
	assert.Empty(t, list(servers.ListOpts{Name: "^fixture$"}))
	assert.Empty(t, list(servers.ListOpts{Name: "Renamed"}))
	assert.Empty(t, list(servers.ListOpts{Name: "^renamed-fixture$", Status: "SHUTOFF"}))
	assert.Equal(t, []servers.Server{*current}, list(servers.ListOpts{Name: "^renamed-fixture$", Status: "ACTIVE"}))
}

func TestNovaDisplayNameListFilterWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	project := routerProjectID(t, provider)
	path := computePath + "/" + project + "/servers/detail"
	auth := map[string]string{"X-Auth-Token": provider.Token(), "OpenStack-API-Version": "compute 2.1"}
	query := url.Values{"name": {"^fixture-server$"}, "status": {"ACTIVE"}}

	// https://docs.openstack.org/api-ref/compute/#list-servers-detailed
	// https://github.com/openstack/nova/blob/master/nova/db/main/api.py
	status, headers, body := request(t, instance, http.MethodGet, path+"?"+query.Encode(), "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	assert.Equal(t, "compute 2.1", headers.Get("OpenStack-API-Version"))
	var matching struct {
		Servers []wireComputeServer `json:"servers"`
	}
	require.NoError(t, json.Unmarshal(body, &matching))
	require.Len(t, matching.Servers, 1)
	assert.Equal(t, fixtureServerID, matching.Servers[0].ID)
	assert.Equal(t, "fixture-server", matching.Servers[0].Name)
	assert.Equal(t, "ACTIVE", matching.Servers[0].Status)

	query.Set("status", "SHUTOFF")
	status, headers, body = request(t, instance, http.MethodGet, path+"?"+query.Encode(), "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	assert.JSONEq(t, `{"servers":[]}`, string(body))

	for _, invalid := range []string{
		"name=", "name=%5B", "name=%28", "name=fixture&name=server", "name=fixture&name=fixture",
	} {
		status, headers, body := request(t, instance, http.MethodGet, path+"?"+invalid, "", auth)
		require.Equal(t, http.StatusBadRequest, status, "%s: %s", invalid, body)
		requireJSON(t, headers)
		var fault struct {
			BadRequest struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"badRequest"`
		}
		require.NoError(t, json.Unmarshal(body, &fault))
		assert.Equal(t, http.StatusBadRequest, fault.BadRequest.Code)
		assert.NotEmpty(t, fault.BadRequest.Message)
	}
}
