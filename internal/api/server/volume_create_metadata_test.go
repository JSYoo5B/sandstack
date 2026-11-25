package server_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/snapshots"
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/volumes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGophercloudCreatesVolumesWithMetadata(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	storage, err := openstack.NewBlockStorageV3(provider, gophercloud.EndpointOpts{
		Region: "RegionOne", Availability: gophercloud.AvailabilityPublic,
	})
	require.NoError(t, err)
	admin := map[string]string{"X-SandStack-Admin-Token": adminToken}
	source, err := snapshots.Create(t.Context(), storage, snapshots.CreateOpts{
		VolumeID: fixtureVolumeID,
		Metadata: map[string]string{"source/only": "snapshot metadata"},
	}).Extract()
	require.NoError(t, err)
	require.NotEmpty(t, source.ID)
	status, _, body := request(t, instance, http.MethodPut,
		snapshotControlPath(source.ID, "complete-create"), "", admin)
	require.Equal(t, http.StatusNoContent, status, "%s", body)

	for _, test := range []struct {
		name string
		opts volumes.CreateOpts
		size int
	}{
		{
			name: "blank volume",
			opts: volumes.CreateOpts{
				Size: 2, Metadata: map[string]string{"scope/key": strings.Repeat("한", 255), "empty": ""},
			},
			size: 2,
		},
		{
			name: "snapshot-sourced volume",
			opts: volumes.CreateOpts{
				SnapshotID: source.ID, Metadata: map[string]string{"scope/key": "restore", "empty": ""},
			},
			size: source.Size,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			created, err := volumes.Create(t.Context(), storage, test.opts, nil).Extract()
			require.NoError(t, err)
			require.NotEmpty(t, created.ID)

			assert.Equal(t, "creating", created.Status)
			assert.Equal(t, test.opts.SnapshotID, created.SnapshotID)
			assert.Equal(t, test.size, created.Size)
			assert.Equal(t, test.opts.Metadata, created.Metadata)
			pending, err := volumes.Get(t.Context(), storage, created.ID).Extract()
			require.NoError(t, err)
			assert.Equal(t, created, pending)

			status, _, body := request(t, instance, http.MethodPut,
				volumeCompleteCreatePath(created.ID), "", admin)
			require.Equal(t, http.StatusNoContent, status, "%s", body)
			completed, err := volumes.Get(t.Context(), storage, created.ID).Extract()
			require.NoError(t, err)
			assert.Equal(t, "available", completed.Status)
			assert.Equal(t, test.opts.SnapshotID, completed.SnapshotID)
			assert.Equal(t, test.size, completed.Size)
			assert.Equal(t, test.opts.Metadata, completed.Metadata)
		})
	}
}

func TestVolumeCreateMetadataWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	collection := volumePath + "/" + volumeProjectID(t, provider) + "/volumes"
	auth := map[string]string{"X-Auth-Token": provider.Token(), "OpenStack-API-Version": "volume 3.0"}

	// https://docs.openstack.org/api-ref/block-storage/v3/#create-a-volume
	// https://github.com/openstack/cinder/blob/master/cinder/api/schemas/volumes.py
	for _, invalid := range []string{
		`[]`,
		`"invalid"`,
		`{"key":null}`,
		`{"key":123}`,
		`{"":"empty key"}`,
		`{"unsupported@key":"value"}`,
		fmt.Sprintf(`{%q:"long key"}`, strings.Repeat("a", 256)),
		fmt.Sprintf(`{"long-value":%q}`, strings.Repeat("한", 256)),
	} {
		payload := `{"volume":{"size":1,"metadata":` + invalid + `}}`
		status, headers, body := request(t, instance, http.MethodPost, collection, payload, auth)
		assert.Equal(t, http.StatusBadRequest, status, "%s: %s", invalid, body)
		requireJSON(t, headers)
	}
	status, headers, body := request(t, instance, http.MethodGet, collection+"/detail", "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	var listed struct {
		Volumes []wireVolume `json:"volumes"`
	}
	require.NoError(t, json.Unmarshal(body, &listed))
	require.Len(t, listed.Volumes, 1)
	assert.Equal(t, fixtureVolumeID, listed.Volumes[0].ID)

	for _, payload := range []string{
		`{"volume":{"size":1}}`,
		`{"volume":{"size":1,"metadata":null}}`,
		`{"volume":{"size":1,"metadata":{}}}`,
	} {
		status, headers, body := request(t, instance, http.MethodPost, collection, payload, auth)
		require.Equal(t, http.StatusAccepted, status, "%s: %s", payload, body)
		requireJSON(t, headers)
		assert.Equal(t, "volume 3.0", headers.Get("OpenStack-API-Version"))
		var created struct {
			Volume wireVolume `json:"volume"`
		}
		require.NoError(t, json.Unmarshal(body, &created))
		require.NotEmpty(t, created.Volume.ID)
		assert.Equal(t, "creating", created.Volume.Status)
		assert.Equal(t, map[string]string{}, created.Volume.Metadata)
	}
}
