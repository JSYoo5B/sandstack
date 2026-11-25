package server_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/availabilityzones"
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/volumes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGophercloudCreatesVolumeInDiscoveredAvailabilityZone(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	storage, err := openstack.NewBlockStorageV3(provider, gophercloud.EndpointOpts{
		Region:       "RegionOne",
		Availability: gophercloud.AvailabilityPublic,
	})
	require.NoError(t, err)

	pages, err := availabilityzones.List(storage).AllPages(t.Context())
	require.NoError(t, err)
	zones, err := availabilityzones.ExtractAvailabilityZones(pages)
	require.NoError(t, err)
	require.Len(t, zones, 1)
	selected := zones[0]
	require.Equal(t, "nova", selected.ZoneName)
	require.True(t, selected.ZoneState.Available)

	created, err := volumes.Create(t.Context(), storage, volumes.CreateOpts{
		Name:             "volume-in-discovered-zone",
		Size:             1,
		AvailabilityZone: selected.ZoneName,
	}, nil).Extract()
	require.NoError(t, err)
	require.NotEmpty(t, created.ID)
	read, err := volumes.Get(t.Context(), storage, created.ID).Extract()
	require.NoError(t, err)

	assert.Equal(t, selected.ZoneName, created.AvailabilityZone)
	assert.Equal(t, selected.ZoneName, read.AvailabilityZone)
	assert.Equal(t, "creating", read.Status)
}

func TestCinderAvailabilityZoneListWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	projectID := volumeProjectID(t, provider)
	const zonePath = "/os-availability-zone"
	endpoint := volumePath + "/" + projectID + zonePath
	auth := map[string]string{"X-Auth-Token": provider.Token()}

	// https://docs.openstack.org/api-ref/block-storage/v3/#list-availability-zone-information
	for _, query := range []string{"", "?unrecognized=value"} {
		status, headers, body := request(t, instance, http.MethodGet, endpoint+query, "", auth)

		require.Equal(t, http.StatusOK, status, "%s", body)
		requireJSON(t, headers)
		assert.Equal(t, "volume 3.0", headers.Get("OpenStack-API-Version"))
		assert.JSONEq(t, `{"availabilityZoneInfo":[{"zoneName":"nova","zoneState":{"available":true}}]}`, string(body))
	}

	for _, tt := range []struct {
		name, path, fault string
		headers           map[string]string
		status            int
	}{
		{
			name: "missing token", path: endpoint, fault: "unauthorized",
			status: http.StatusUnauthorized,
		},
		{
			name: "invalid token", path: endpoint, fault: "unauthorized",
			headers: map[string]string{"X-Auth-Token": "invalid-token"},
			status:  http.StatusUnauthorized,
		},
		{
			name: "wrong URL project", path: volumePath + "/00000000-0000-0000-0000-000000000000" + zonePath,
			fault: "badRequest", headers: auth, status: http.StatusBadRequest,
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
			require.Contains(t, response, tt.fault)
			assert.Equal(t, tt.status, response[tt.fault].Code)
			assert.NotEmpty(t, response[tt.fault].Message)
		})
	}
}
