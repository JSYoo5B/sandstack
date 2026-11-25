package server_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/snapshots"
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/volumes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVolumeFromSnapshotWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	base := volumePath + "/" + volumeProjectID(t, provider)
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	admin := map[string]string{"X-SandStack-Admin-Token": adminToken}
	status, _, body := request(t, instance, http.MethodPost, base+"/snapshots",
		`{"snapshot":{"volume_id":"`+fixtureVolumeID+`"}}`, auth)
	require.Equal(t, http.StatusAccepted, status, "%s", body)
	var source struct {
		Snapshot wireSnapshot `json:"snapshot"`
	}
	require.NoError(t, json.Unmarshal(body, &source))
	require.NotEmpty(t, source.Snapshot.ID)
	require.Equal(t, 1, source.Snapshot.Size)
	status, _, body = request(t, instance, http.MethodPut, snapshotControlPath(source.Snapshot.ID, "complete-create"), "", admin)
	require.Equal(t, http.StatusNoContent, status, "%s", body)

	for _, invalid := range []struct {
		name   string
		body   string
		status int
	}{
		{"blank volume without size", `{"volume":{"size":null}}`, http.StatusBadRequest},
		{"snapshot ID outside fixture scope", `{"volume":{"size":1,"snapshot_id":null}}`, http.StatusBadRequest},
		{"missing snapshot", `{"volume":{"snapshot_id":"00000000-0000-0000-0000-000000000000"}}`, http.StatusNotFound},
		{"explicit zero size", fmt.Sprintf(`{"volume":{"size":0,"snapshot_id":%q}}`, source.Snapshot.ID), http.StatusBadRequest},
	} {
		t.Run(invalid.name, func(t *testing.T) {
			status, headers, body := request(t, instance, http.MethodPost, base+"/volumes", invalid.body, auth)
			assert.Equal(t, invalid.status, status, "%s", body)
			requireJSON(t, headers)
		})
	}

	// https://raw.githubusercontent.com/openstack/cinder/master/cinder/api/schemas/volumes.py
	// https://raw.githubusercontent.com/openstack/cinder/master/cinder/api/v3/volumes.py
	payload := fmt.Sprintf(`{"volume":{"name":"restore-from-snapshot","snapshot_id":%q,"size":null}}`, source.Snapshot.ID)
	status, headers, body := request(t, instance, http.MethodPost, base+"/volumes", payload, auth)
	require.Equal(t, http.StatusAccepted, status, "%s", body)
	requireJSON(t, headers)
	type responseEnvelope struct {
		Volume struct {
			ID         string          `json:"id"`
			SnapshotID string          `json:"snapshot_id"`
			Name       string          `json:"name"`
			Size       int             `json:"size"`
			Status     string          `json:"status"`
			UpdatedAt  json.RawMessage `json:"updated_at"`
		} `json:"volume"`
	}
	var response, detail responseEnvelope
	require.NoError(t, json.Unmarshal(body, &response))
	created := response.Volume
	require.NotEmpty(t, created.ID)
	assert.NotEqual(t, source.Snapshot.ID, created.ID)
	assert.NotEqual(t, fixtureVolumeID, created.ID)
	assert.Equal(t, source.Snapshot.ID, created.SnapshotID)
	assert.Equal(t, "restore-from-snapshot", created.Name)
	assert.Equal(t, 1, created.Size)
	assert.Equal(t, "creating", created.Status)
	assert.JSONEq(t, "null", string(created.UpdatedAt))
	status, _, body = request(t, instance, http.MethodGet, base+"/volumes/"+created.ID, "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	require.NoError(t, json.Unmarshal(body, &detail))
	assert.Equal(t, created, detail.Volume)
}

func TestGophercloudCreatesIndependentVolumesFromSnapshot(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	storage, err := openstack.NewBlockStorageV3(provider, gophercloud.EndpointOpts{
		Region: "RegionOne", Availability: gophercloud.AvailabilityPublic,
	})
	require.NoError(t, err)
	createVolume := func(opts volumes.CreateOpts) *volumes.Volume {
		t.Helper()
		result, err := volumes.Create(t.Context(), storage, opts, nil).Extract()
		require.NoError(t, err)
		require.NotNil(t, result)
		require.NotEmpty(t, result.ID)
		return result
	}
	createSnapshot := func(volumeID, name string) *snapshots.Snapshot {
		t.Helper()
		result, err := snapshots.Create(t.Context(), storage, snapshots.CreateOpts{
			VolumeID: volumeID, Name: name,
		}).Extract()
		require.NoError(t, err)
		require.NotNil(t, result)
		require.NotEmpty(t, result.ID)
		return result
	}
	listVolumes := func() []volumes.Volume {
		t.Helper()
		pages, err := volumes.List(storage, nil).AllPages(t.Context())
		require.NoError(t, err)
		result, err := volumes.ExtractVolumes(pages)
		require.NoError(t, err)
		return result
	}
	read := func(id string) *volumes.Volume {
		t.Helper()
		result, err := volumes.Get(t.Context(), storage, id).Extract()
		require.NoError(t, err)
		require.NotNil(t, result)
		return result
	}
	admin := map[string]string{"X-SandStack-Admin-Token": adminToken}
	control := func(method, path string) {
		t.Helper()
		status, _, body := request(t, instance, method, path, "", admin)
		require.Equal(t, http.StatusNoContent, status, "%s", body)
	}

	sourceVolume := createVolume(volumes.CreateOpts{Size: 5, Name: "source-disk"})
	control(http.MethodPut, volumeCompleteCreatePath(sourceVolume.ID))
	source := createSnapshot(sourceVolume.ID, "source-snapshot")
	failed := createSnapshot(sourceVolume.ID, "failed-snapshot")
	control(http.MethodPut, snapshotControlPath(failed.ID, "fail-create"))
	before := listVolumes()
	const missingID = "00000000-0000-0000-0000-000000000000"
	for _, invalid := range []struct {
		name       string
		snapshotID string
		status     int
	}{
		{"pending snapshot", source.ID, http.StatusBadRequest},
		{"failed snapshot", failed.ID, http.StatusBadRequest},
		{"missing snapshot", missingID, http.StatusNotFound},
	} {
		t.Run(invalid.name, func(t *testing.T) {
			_, err := volumes.Create(t.Context(), storage, volumes.CreateOpts{SnapshotID: invalid.snapshotID}, nil).Extract()
			require.Error(t, err)
			assert.True(t, gophercloud.ResponseCodeIs(err, invalid.status), "%v", err)
		})
	}
	control(http.MethodPut, snapshotControlPath(source.ID, "complete-create"))
	_, err = volumes.Create(t.Context(), storage, volumes.CreateOpts{SnapshotID: source.ID, Size: 4}, nil).Extract()
	require.Error(t, err)
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusBadRequest), "%v", err)
	assert.ElementsMatch(t, before, listVolumes())

	inherited := createVolume(volumes.CreateOpts{SnapshotID: source.ID, Name: "inherited-size"})
	larger := createVolume(volumes.CreateOpts{SnapshotID: source.ID, Size: 7, Name: "larger-size"})
	assert.Equal(t, 5, inherited.Size)
	assert.Equal(t, 7, larger.Size)
	for _, created := range []*volumes.Volume{inherited, larger} {
		assert.Equal(t, "creating", created.Status)
		assert.Equal(t, source.ID, created.SnapshotID)
		assert.Equal(t, created, read(created.ID))
	}
	wantList := append(slices.Clone(before), *inherited, *larger)
	assert.ElementsMatch(t, wantList, listVolumes())

	require.NoError(t, snapshots.Delete(t.Context(), storage, source.ID).ExtractErr())
	_, err = snapshots.Get(t.Context(), storage, source.ID).Extract()
	require.Error(t, err)
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusNotFound), "%v", err)
	assert.Equal(t, inherited, read(inherited.ID))
	assert.Equal(t, larger, read(larger.ID))
	control(http.MethodPut, volumeCompleteCreatePath(inherited.ID))
	available := read(inherited.ID)
	assert.Equal(t, "available", available.Status)
	assert.Equal(t, source.ID, available.SnapshotID)
	assert.Equal(t, 5, available.Size)
	control(http.MethodPut, "/_sandstack/volume/volumes/"+inherited.ID+"/fault")
	faulted := read(inherited.ID)
	assert.Equal(t, "error", faulted.Status)
	assert.Equal(t, source.ID, faulted.SnapshotID)
	assert.Equal(t, 5, faulted.Size)
	assert.Equal(t, larger, read(larger.ID))
}
