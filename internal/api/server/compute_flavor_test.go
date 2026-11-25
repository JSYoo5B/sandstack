package server_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/flavors"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/servers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGophercloudFollowsNovaFlavorReference(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	compute, err := openstack.NewComputeV2(provider, gophercloud.EndpointOpts{
		Region:       "RegionOne",
		Availability: gophercloud.AvailabilityPublic,
	})
	require.NoError(t, err)

	server, err := servers.Get(t.Context(), compute, fixtureServerID).Extract()
	require.NoError(t, err)
	flavorID, ok := server.Flavor["id"].(string)
	require.True(t, ok, "unexpected flavor ID in %#v", server.Flavor)
	require.Equal(t, "1", flavorID)
	flavor, err := flavors.Get(t.Context(), compute, flavorID).Extract()
	require.NoError(t, err)

	assert.Equal(t, flavorID, flavor.ID)
	assert.Equal(t, "m1.tiny", flavor.Name)
	assert.Equal(t, 512, flavor.RAM)
	assert.Equal(t, 1, flavor.Disk)
	assert.Equal(t, 1, flavor.VCPUs)
	assert.Zero(t, flavor.Ephemeral)
	assert.Zero(t, flavor.Swap)
	assert.Equal(t, 1.0, flavor.RxTxFactor)
	assert.True(t, flavor.IsPublic)
}

func TestNovaFlavorDetailWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	projectID := routerProjectID(t, provider)
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	projectless := "/flavors/1"
	projectScoped := "/" + projectID + projectless

	// https://docs.openstack.org/api-ref/compute/#show-flavor-details
	// https://github.com/openstack/nova/blob/master/nova/api/openstack/compute/views/flavors.py#L39-L68
	for _, tt := range []struct {
		name string
		path string
	}{
		{name: "project scoped", path: projectScoped},
		{name: "projectless", path: projectless},
	} {
		t.Run(tt.name, func(t *testing.T) {
			status, headers, body := request(t, instance, http.MethodGet, computePath+tt.path, "", auth)

			require.Equal(t, http.StatusOK, status, "%s", body)
			requireJSON(t, headers)
			assert.Equal(t, "compute 2.1", headers.Get("OpenStack-API-Version"))
			assert.Equal(t, "2.1", headers.Get("X-OpenStack-Nova-API-Version"))
			var response map[string]map[string]any
			require.NoError(t, json.Unmarshal(body, &response))
			require.Len(t, response, 1)
			require.Contains(t, response, "flavor")
			assert.Equal(t, map[string]any{
				"id":                         "1",
				"name":                       "m1.tiny",
				"ram":                        float64(512),
				"disk":                       float64(1),
				"vcpus":                      float64(1),
				"OS-FLV-EXT-DATA:ephemeral":  float64(0),
				"OS-FLV-DISABLED:disabled":   false,
				"os-flavor-access:is_public": true,
				"rxtx_factor":                float64(1),
				"swap":                       "",
				"links": []any{
					map[string]any{"rel": "self", "href": instance.URL + computePath + tt.path},
					map[string]any{"rel": "bookmark", "href": instance.URL + "/compute" + tt.path},
				},
			}, response["flavor"])
		})
	}

	for _, tt := range []struct {
		name      string
		path      string
		headers   map[string]string
		status    int
		faultType string
	}{
		{
			name: "unknown ID", path: computePath + "/" + projectID + "/flavors/999",
			headers: auth, status: http.StatusNotFound, faultType: "itemNotFound",
		},
		{
			name: "missing token", path: computePath + projectless,
			status: http.StatusUnauthorized, faultType: "unauthorized",
		},
		{
			name: "invalid token", path: computePath + projectScoped,
			headers: map[string]string{"X-Auth-Token": "invalid"},
			status:  http.StatusUnauthorized, faultType: "unauthorized",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			status, headers, body := request(t, instance, http.MethodGet, tt.path, "", tt.headers)

			require.Equal(t, tt.status, status, "%s", body)
			requireJSON(t, headers)
			var response map[string]struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			}
			require.NoError(t, json.Unmarshal(body, &response))
			require.Contains(t, response, tt.faultType)
			assert.Equal(t, tt.status, response[tt.faultType].Code)
			assert.NotEmpty(t, response[tt.faultType].Message)
		})
	}
}
