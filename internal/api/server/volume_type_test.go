package server_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/volumes"
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/volumetypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const fixtureVolumeTypeID = "3190e22a-9606-4cf7-b9bd-4eb5af3dc601"

func TestGophercloudCreatesVolumeWithDiscoveredDefaultType(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	service, err := openstack.NewBlockStorageV3(provider, gophercloud.EndpointOpts{
		Region:       "RegionOne",
		Availability: gophercloud.AvailabilityPublic,
	})
	require.NoError(t, err)

	defaultType, err := volumetypes.Get(t.Context(), service, "default").Extract()
	require.NoError(t, err)
	require.Equal(t, fixtureVolumeTypeID, defaultType.ID)
	require.Equal(t, "standard", defaultType.Name)
	assert.True(t, defaultType.IsPublic)
	assert.Equal(t, map[string]string{}, defaultType.ExtraSpecs)
	assert.Equal(t, "Standard SandStack volume type", defaultType.Description)
	assert.False(t, defaultType.PublicAccess)

	byID, err := volumetypes.Get(t.Context(), service, defaultType.ID).Extract()
	require.NoError(t, err)
	expected := *defaultType
	expected.PublicAccess = true
	assert.Equal(t, expected, *byID)

	created, err := volumes.Create(t.Context(), service, volumes.CreateOpts{
		Name:       "volume-with-discovered-type",
		Size:       1,
		VolumeType: defaultType.Name,
	}, nil).Extract()
	require.NoError(t, err)
	persisted, err := volumes.Get(t.Context(), service, created.ID).Extract()
	require.NoError(t, err)

	assert.Equal(t, defaultType.Name, created.VolumeType)
	assert.Equal(t, defaultType.Name, persisted.VolumeType)
	assert.Equal(t, "creating", persisted.Status)
}

func TestCinderVolumeTypeDetailWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	projectID := volumeProjectID(t, provider)
	base := volumePath + "/" + projectID + "/types/"
	auth := map[string]string{"X-Auth-Token": provider.Token()}

	// https://docs.openstack.org/api-ref/block-storage/v3/#show-default-volume-type
	// https://github.com/openstack/cinder/blob/master/cinder/api/v3/views/types.py#L20-L44
	for _, tt := range []struct {
		selector    string
		legacyField bool
	}{
		{selector: "default"},
		{selector: fixtureVolumeTypeID, legacyField: true},
	} {
		t.Run(tt.selector, func(t *testing.T) {
			status, headers, body := request(t, instance, http.MethodGet, base+tt.selector, "", auth)

			require.Equal(t, http.StatusOK, status, "%s", body)
			requireJSON(t, headers)
			assert.Equal(t, "volume 3.0", headers.Get("OpenStack-API-Version"))
			var response map[string]map[string]any
			require.NoError(t, json.Unmarshal(body, &response))
			require.Len(t, response, 1)
			expected := map[string]any{
				"id":          fixtureVolumeTypeID,
				"name":        "standard",
				"description": "Standard SandStack volume type",
				"is_public":   true,
				"extra_specs": map[string]any{},
			}
			if tt.legacyField {
				expected["os-volume-type-access:is_public"] = true
			}
			assert.Equal(t, expected, response["volume_type"])
		})
	}

	const unknownID = "00000000-0000-0000-0000-000000000000"
	for _, tt := range []struct {
		name      string
		path      string
		headers   map[string]string
		status    int
		faultType string
	}{
		{
			name: "unknown ID", path: base + unknownID, headers: auth,
			status: http.StatusNotFound, faultType: "itemNotFound",
		},
		{
			name: "concrete type name is not a selector", path: base + "standard", headers: auth,
			status: http.StatusNotFound, faultType: "itemNotFound",
		},
		{
			name: "missing token", path: base + "default",
			status: http.StatusUnauthorized, faultType: "unauthorized",
		},
		{
			name: "invalid token", path: base + fixtureVolumeTypeID,
			headers: map[string]string{"X-Auth-Token": "invalid-token"},
			status:  http.StatusUnauthorized, faultType: "unauthorized",
		},
		{
			name: "wrong URL project", path: volumePath + "/" + unknownID + "/types/default", headers: auth,
			status: http.StatusBadRequest, faultType: "badRequest",
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
