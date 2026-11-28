package server_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/snapshots"
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/volumes"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/volumeattach"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/volume/manager.py#L3071-L3225
func TestGophercloudRetypesAvailableAndAttachedVolume(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	endpoint := gophercloud.EndpointOpts{Region: "RegionOne", Availability: gophercloud.AvailabilityPublic}
	storage, err := openstack.NewBlockStorageV3(provider, endpoint)
	require.NoError(t, err)
	compute, err := openstack.NewComputeV2(provider, endpoint)
	require.NoError(t, err)
	ctx := t.Context()
	admin := map[string]string{"X-SandStack-Admin-Token": adminToken}
	read := func() *volumes.Volume {
		t.Helper()
		volume, err := volumes.Get(ctx, storage, fixtureVolumeID).Extract()
		require.NoError(t, err)
		return volume
	}
	baseline := read()
	require.Equal(t, "standard", baseline.VolumeType)
	require.Equal(t, "available", baseline.Status)
	snapshot, err := snapshots.Create(ctx, storage, snapshots.CreateOpts{VolumeID: fixtureVolumeID}).Extract()
	require.NoError(t, err)
	status, _, body := request(t, instance, http.MethodPut, snapshotControlPath(snapshot.ID, "complete-create"), "", admin)
	require.Equal(t, http.StatusNoContent, status, "%s", body)

	err = volumes.ChangeType(ctx, storage, fixtureVolumeID, volumes.ChangeTypeOpts{
		NewType: "performance", MigrationPolicy: volumes.MigrationPolicyNever,
	}).ExtractErr()
	require.NoError(t, err)
	retyped := read()
	sameErr := volumes.ChangeType(ctx, storage, fixtureVolumeID, volumes.ChangeTypeOpts{
		NewType: performanceVolumeTypeID,
	}).ExtractErr()
	unknownErr := volumes.ChangeType(ctx, storage, fixtureVolumeID, volumes.ChangeTypeOpts{
		NewType: "missing-type",
	}).ExtractErr()
	clone, err := volumes.Create(ctx, storage, volumes.CreateOpts{SnapshotID: snapshot.ID}, nil).Extract()
	require.NoError(t, err)

	assert.Equal(t, "performance", retyped.VolumeType)
	assert.Equal(t, "available", retyped.Status)
	assert.False(t, retyped.UpdatedAt.Before(baseline.UpdatedAt))
	assert.True(t, gophercloud.ResponseCodeIs(sameErr, http.StatusBadRequest), "%v", sameErr)
	assert.True(t, gophercloud.ResponseCodeIs(unknownErr, http.StatusNotFound), "%v", unknownErr)
	assert.Equal(t, "performance", clone.VolumeType)

	_, err = volumeattach.Create(ctx, compute, fixtureServerID, volumeattach.CreateOpts{
		VolumeID: fixtureVolumeID,
	}).Extract()
	require.NoError(t, err)
	require.Equal(t, "in-use", read().Status)
	err = volumes.ChangeType(ctx, storage, fixtureVolumeID, volumes.ChangeTypeOpts{
		NewType: fixtureVolumeTypeID, MigrationPolicy: volumes.MigrationPolicyOnDemand,
	}).ExtractErr()
	require.NoError(t, err)
	attached := read()

	assert.Equal(t, "standard", attached.VolumeType)
	assert.Equal(t, "in-use", attached.Status)
	assert.Len(t, attached.Attachments, 1)
}

// https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/api/contrib/volume_actions.py#L307-L317
// https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/api/schemas/volume_actions.py#L125-L142
// https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/tests/unit/api/contrib/test_volume_actions.py#L513-L607
func TestCinderVolumeRetypeWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	collection := volumePath + "/" + volumeProjectID(t, provider) + "/volumes"
	detail := collection + "/" + fixtureVolumeID
	action := detail + "/action"
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	admin := map[string]string{"X-SandStack-Admin-Token": adminToken}
	read := func(path string) wireVolume {
		t.Helper()
		status, _, body := request(t, instance, http.MethodGet, path, "", auth)
		require.Equal(t, http.StatusOK, status, "%s", body)
		var response struct {
			Volume wireVolume `json:"volume"`
		}
		require.NoError(t, json.Unmarshal(body, &response))
		return response.Volume
	}
	status, _, body := request(t, instance, http.MethodPost, collection,
		`{"volume":{"size":1,"volume_type":"performance"}}`, auth)
	require.Equal(t, http.StatusAccepted, status, "%s", body)
	var created struct {
		Volume wireVolume `json:"volume"`
	}
	require.NoError(t, json.Unmarshal(body, &created))
	require.Equal(t, "performance", created.Volume.VolumeType)
	baseline := read(detail)

	for _, tc := range []struct {
		name   string
		path   string
		body   string
		status int
	}{
		{"null action", action, `{"os-retype":null}`, http.StatusBadRequest},
		{"missing type", action, `{"os-retype":{}}`, http.StatusBadRequest},
		{"numeric type", action, `{"os-retype":{"new_type":1}}`, http.StatusBadRequest},
		{"null policy", action, `{"os-retype":{"new_type":"performance","migration_policy":null}}`, http.StatusBadRequest},
		{"unknown policy", action, `{"os-retype":{"new_type":"performance","migration_policy":"invalid"}}`, http.StatusBadRequest},
		{"extra field", action, `{"os-retype":{"new_type":"performance","force":true}}`, http.StatusBadRequest},
		{"same type", action, `{"os-retype":{"new_type":"standard"}}`, http.StatusBadRequest},
		{"creating volume", collection + "/" + created.Volume.ID + "/action", `{"os-retype":{"new_type":"standard"}}`, http.StatusBadRequest},
		{"unknown type", action, `{"os-retype":{"new_type":"fake_vol_type"}}`, http.StatusNotFound},
		{"default alias", action, `{"os-retype":{"new_type":"default"}}`, http.StatusNotFound},
		{"missing volume", collection + "/" + missingVolumeMarker + "/action", `{"os-retype":{"new_type":"performance"}}`, http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, headers, body := request(t, instance, http.MethodPost, tc.path, tc.body, auth)

			assert.Equal(t, tc.status, status, "%s", body)
			requireJSON(t, headers)
		})
	}
	assert.Equal(t, baseline, read(detail))

	status, _, body = request(t, instance, http.MethodPost, action,
		`{"os-retype":{"new_type":"`+performanceVolumeTypeID+`","migration_policy":"on-demand"}}`, auth)
	require.Equal(t, http.StatusAccepted, status, "%s", body)
	assert.Empty(t, body)
	retyped := read(detail)

	assert.Equal(t, "performance", retyped.VolumeType)
	assert.Equal(t, "available", retyped.Status)
	assert.GreaterOrEqual(t, retyped.UpdatedAt, baseline.UpdatedAt)

	status, _, body = request(t, instance, http.MethodPost, "/_sandstack/reset", "", admin)
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	auth["X-Auth-Token"] = authenticate(t, instance).Token()
	assert.Equal(t, "standard", read(detail).VolumeType)
}
