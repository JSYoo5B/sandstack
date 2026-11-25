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

func TestGophercloudDiscoversNovaServersViaBasicList(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	compute, err := openstack.NewComputeV2(provider, gophercloud.EndpointOpts{
		Region:       "RegionOne",
		Availability: gophercloud.AvailabilityPublic,
	})
	require.NoError(t, err)
	ctx := t.Context()
	baseline, err := servers.Get(ctx, compute, fixtureServerID).Extract()
	require.NoError(t, err)

	status, _, body := request(t, instance, http.MethodPut, unavailableControl, "", map[string]string{
		"X-SandStack-Admin-Token": adminToken,
	})
	require.Equal(t, http.StatusNoContent, status, "%s", body)

	pages, err := servers.ListSimple(compute, servers.ListOpts{
		Name: "^fixture-server$", Status: "ACTIVE",
	}).AllPages(ctx)
	require.NoError(t, err)
	rows, err := servers.ExtractServers(pages)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, fixtureServerID, rows[0].ID)
	assert.Equal(t, "fixture-server", rows[0].Name)
	assert.Equal(t, baseline.Links, rows[0].Links)
	assert.Empty(t, rows[0].Status)

	for _, opts := range []servers.ListOpts{
		{Name: "^fixture-server$", Status: "SHUTOFF"},
		{Name: "^other$", Status: "ACTIVE"},
	} {
		pages, err := servers.ListSimple(compute, opts).AllPages(ctx)
		require.NoError(t, err)
		rows, err := servers.ExtractServers(pages)
		require.NoError(t, err)
		assert.Empty(t, rows)
	}

	_, err = servers.Get(ctx, compute, rows[0].ID).Extract()
	require.True(t, gophercloud.ResponseCodeIs(err, http.StatusServiceUnavailable), "%v", err)
	recovered, err := servers.Get(ctx, compute, rows[0].ID).Extract()
	require.NoError(t, err)
	assert.Equal(t, baseline, recovered)
}

func TestNovaBasicServerListWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	project := routerProjectID(t, provider)
	auth := map[string]string{"X-Auth-Token": provider.Token()}

	query := url.Values{"name": {"^fixture-server$"}, "status": {"ACTIVE"}}
	// https://docs.openstack.org/api-ref/compute/#list-servers
	// https://github.com/openstack/nova/blob/master/nova/api/openstack/compute/views/servers.py#L93-L125
	for _, tt := range []struct{ name, prefix string }{
		{name: "projectless", prefix: ""},
		{name: "project scoped", prefix: "/" + project},
	} {
		t.Run(tt.name, func(t *testing.T) {
			collection := tt.prefix + "/servers"
			path := computePath + collection
			status, headers, body := request(t, instance, http.MethodGet, path+"?"+query.Encode(), "", auth)

			require.Equal(t, http.StatusOK, status, "%s", body)
			requireJSON(t, headers)
			assert.Equal(t, "compute 2.1", headers.Get("OpenStack-API-Version"))
			assert.Equal(t, "2.1", headers.Get("X-OpenStack-Nova-API-Version"))
			resource := collection + "/" + fixtureServerID
			expected, err := json.Marshal(map[string][]map[string]any{"servers": {{
				"id":   fixtureServerID,
				"name": "fixture-server",
				"links": []computeLink{
					{Rel: "self", Href: instance.URL + computePath + resource},
					{Rel: "bookmark", Href: instance.URL + "/compute" + resource},
				},
			}}})
			require.NoError(t, err)
			assert.JSONEq(t, string(expected), string(body))
		})
	}

	base := computePath + "/" + project + "/servers"
	status, headers, body := request(t, instance, http.MethodGet, base+"?status=SHUTOFF", "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	assert.JSONEq(t, `{"servers":[]}`, string(body))

	for _, tt := range []struct {
		name, path, fault string
		headers           map[string]string
		status            int
	}{
		{
			name: "missing token", path: base, fault: "unauthorized",
			status: http.StatusUnauthorized,
		},
		{
			name: "wrong URL project", path: computePath + "/another-project/servers", fault: "badRequest",
			headers: auth, status: http.StatusBadRequest,
		},
		{
			name: "repeated name", path: base + "?name=fixture&name=server", fault: "badRequest",
			headers: auth, status: http.StatusBadRequest,
		},
		{
			name: "repeated status", path: base + "?status=ACTIVE&status=ERROR", fault: "badRequest",
			headers: auth, status: http.StatusBadRequest,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			status, headers, body := request(t, instance, http.MethodGet, tt.path, "", tt.headers)

			require.Equal(t, tt.status, status, "%s", body)
			requireJSON(t, headers)
			var fault map[string]struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			}
			require.NoError(t, json.Unmarshal(body, &fault))
			require.Contains(t, fault, tt.fault)
			assert.Equal(t, tt.status, fault[tt.fault].Code)
			assert.NotEmpty(t, fault[tt.fault].Message)
		})
	}
}
