package server_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/snapshots"
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/volumes"
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/volumetypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGophercloudCreatesVolumesWithDiscoveredTypeID(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	storage, err := openstack.NewBlockStorageV3(provider, gophercloud.EndpointOpts{
		Region:       "RegionOne",
		Availability: gophercloud.AvailabilityPublic,
	})
	require.NoError(t, err)
	defaultType, err := volumetypes.Get(t.Context(), storage, "default").Extract()
	require.NoError(t, err)
	require.Equal(t, fixtureVolumeTypeID, defaultType.ID)
	require.Equal(t, "standard", defaultType.Name)

	snapshot, err := snapshots.Create(t.Context(), storage, snapshots.CreateOpts{
		VolumeID: fixtureVolumeID,
	}).Extract()
	require.NoError(t, err)
	require.NotEmpty(t, snapshot.ID)
	status, _, body := request(t, instance, http.MethodPut,
		snapshotControlPath(snapshot.ID, "complete-create"), "", map[string]string{
			"X-SandStack-Admin-Token": adminToken,
		})
	require.Equal(t, http.StatusNoContent, status, "%s", body)

	for _, opts := range []volumes.CreateOpts{
		{Name: "blank", Size: 2, VolumeType: defaultType.ID},
		{Name: "from-snapshot", SnapshotID: snapshot.ID, VolumeType: defaultType.ID},
	} {
		t.Run(opts.Name, func(t *testing.T) {
			created, err := volumes.Create(t.Context(), storage, opts, nil).Extract()
			require.NoError(t, err)
			require.NotEmpty(t, created.ID)
			got, err := volumes.Get(t.Context(), storage, created.ID).Extract()
			require.NoError(t, err)

			assert.Equal(t, defaultType.Name, created.VolumeType)
			assert.Equal(t, created.VolumeType, got.VolumeType)
			assert.Equal(t, opts.SnapshotID, got.SnapshotID)
			assert.Equal(t, "creating", got.Status)
		})
	}
}

func TestCinderVolumeTypeCreateWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	collection := volumePath + "/" + volumeProjectID(t, provider) + "/volumes"
	auth := map[string]string{"X-Auth-Token": provider.Token()}

	// https://github.com/openstack/cinder/blob/master/cinder/api/v3/volumes.py#L360-L375
	// https://github.com/openstack/cinder/blob/master/cinder/api/schemas/volumes.py#L40-L43
	for _, tt := range []struct {
		name   string
		value  string
		status int
		fault  string
	}{
		{"unknown name", `"missing-type"`, http.StatusNotFound, "itemNotFound"},
		{"unknown UUID", `"00000000-0000-0000-0000-000000000000"`, http.StatusNotFound, "itemNotFound"},
		{"lookup alias", `"default"`, http.StatusNotFound, "itemNotFound"},
		{"number", `12`, http.StatusBadRequest, "badRequest"},
		{"overlength type", `"` + strings.Repeat("a", 256) + `"`, http.StatusBadRequest, "badRequest"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			payload := `{"volume":{"size":1,"volume_type":` + tt.value + `}}`
			status, headers, body := request(t, instance, http.MethodPost, collection, payload, auth)

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

	status, _, body := request(t, instance, http.MethodGet, collection+"/detail", "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	var listed struct {
		Volumes []wireVolume `json:"volumes"`
	}
	require.NoError(t, json.Unmarshal(body, &listed))
	require.Len(t, listed.Volumes, 1)
	assert.Equal(t, fixtureVolumeID, listed.Volumes[0].ID)

	for _, tt := range []struct {
		name  string
		field string
	}{
		{name: "omitted"},
		{name: "null", field: `,"volume_type":null`},
		{name: "empty", field: `,"volume_type":""`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			payload := `{"volume":{"size":1` + tt.field + `}}`
			status, headers, body := request(t, instance, http.MethodPost, collection, payload, auth)

			require.Equal(t, http.StatusAccepted, status, "%s", body)
			requireJSON(t, headers)
			var response struct {
				Volume wireVolume `json:"volume"`
			}
			require.NoError(t, json.Unmarshal(body, &response))
			assert.NotEmpty(t, response.Volume.ID)
			assert.Equal(t, "standard", response.Volume.VolumeType)
		})
	}
}
