package server_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/snapshots"
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/transfers"
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/volumes"
	"github.com/gophercloud/gophercloud/v2/openstack/identity/v3/tokens"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fixtureUserID(t *testing.T, provider *gophercloud.ProviderClient) string {
	t.Helper()
	result, ok := provider.GetAuthResult().(tokens.CreateResult)
	require.True(t, ok)
	user, err := result.ExtractUser()
	require.NoError(t, err)
	return user.ID
}

// https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/volume/api.py#L598-L686
// https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/transfer/api.py#L235-L351
// https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/db/api.py#L6996-L7061
func TestGophercloudTransfersVolumeToAnotherProject(t *testing.T) {
	instance := startServerWithSecondFixture(t, "")
	endpoint := gophercloud.EndpointOpts{Region: "RegionOne", Availability: gophercloud.AvailabilityPublic}
	primary := authenticate(t, instance)
	second := authenticateSecond(t, instance)
	donor, err := openstack.NewBlockStorageV3(primary, endpoint)
	require.NoError(t, err)
	receiver, err := openstack.NewBlockStorageV3(second, endpoint)
	require.NoError(t, err)
	ctx := t.Context()
	admin := map[string]string{"X-SandStack-Admin-Token": adminToken}
	listIDs := func(client *gophercloud.ServiceClient) []string {
		t.Helper()
		pages, err := volumes.List(client, volumes.ListOpts{}).AllPages(ctx)
		require.NoError(t, err)
		listed, err := volumes.ExtractVolumes(pages)
		require.NoError(t, err)
		ids := make([]string, len(listed))
		for i, volume := range listed {
			ids[i] = volume.ID
		}
		return ids
	}

	own, err := volumes.Create(ctx, receiver, volumes.CreateOpts{Name: "receiver", Size: 1}, nil).Extract()
	require.NoError(t, err)
	status, _, body := request(t, instance, http.MethodPut,
		"/_sandstack/volume/volumes/"+own.ID+"/complete-create", "", admin)
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	_, crossReadErr := volumes.Get(ctx, donor, own.ID).Extract()

	assert.Equal(t, volumeProjectID(t, second), own.TenantID)
	assert.Equal(t, fixtureUserID(t, second), own.UserID)
	assert.Equal(t, []string{own.ID}, listIDs(receiver))
	assert.Equal(t, []string{fixtureVolumeID}, listIDs(donor))
	assert.True(t, gophercloud.ResponseCodeIs(crossReadErr, http.StatusNotFound), "%v", crossReadErr)

	snapshot, err := snapshots.Create(ctx, donor, snapshots.CreateOpts{VolumeID: fixtureVolumeID}).Extract()
	require.NoError(t, err)
	status, _, body = request(t, instance, http.MethodPut, snapshotControlPath(snapshot.ID, "complete-create"), "", admin)
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	created, err := transfers.Create(ctx, donor, transfers.CreateOpts{VolumeID: fixtureVolumeID}).Extract()
	require.NoError(t, err)

	accepted, err := transfers.Accept(ctx, receiver, created.ID, transfers.AcceptOpts{AuthKey: created.AuthKey}).Extract()
	require.NoError(t, err)
	moved, err := volumes.Get(ctx, receiver, fixtureVolumeID).Extract()
	require.NoError(t, err)
	movedSnapshot, err := snapshots.Get(ctx, receiver, snapshot.ID).Extract()
	require.NoError(t, err)
	_, donorReadErr := volumes.Get(ctx, donor, fixtureVolumeID).Extract()
	_, donorSnapshotErr := snapshots.Get(ctx, donor, snapshot.ID).Extract()

	assert.Equal(t, fixtureVolumeID, accepted.VolumeID)
	assert.Equal(t, "available", moved.Status)
	assert.Equal(t, volumeProjectID(t, second), moved.TenantID)
	assert.Equal(t, fixtureUserID(t, second), moved.UserID)
	assert.Equal(t, fixtureVolumeID, movedSnapshot.VolumeID)
	assert.True(t, gophercloud.ResponseCodeIs(donorReadErr, http.StatusNotFound), "%v", donorReadErr)
	assert.True(t, gophercloud.ResponseCodeIs(donorSnapshotErr, http.StatusNotFound), "%v", donorSnapshotErr)
	assert.ElementsMatch(t, []string{own.ID, fixtureVolumeID}, listIDs(receiver))
	assert.Empty(t, listIDs(donor))
}

// https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/db/api.py#L331-L370
// https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/db/api.py#L2377-L2395
// https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/db/api.py#L3783-L3797
// https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/db/api.py#L6368-L6390
// https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/db/api.py#L6996-L7061
func TestCinderCrossProjectWire(t *testing.T) {
	instance := startServerWithSecondFixture(t, "")
	primary := authenticate(t, instance)
	second := authenticateSecond(t, instance)
	donorProject := volumePath + "/" + volumeProjectID(t, primary)
	receiverProjectID := volumeProjectID(t, second)
	receiverProject := volumePath + "/" + receiverProjectID
	donor := map[string]string{"X-Auth-Token": primary.Token()}
	receiver := map[string]string{"X-Auth-Token": second.Token()}
	admin := map[string]string{"X-SandStack-Admin-Token": adminToken}
	send := func(method, path, body string, headers map[string]string, want int) map[string]json.RawMessage {
		t.Helper()
		status, _, data := request(t, instance, method, path, body, headers)
		require.Equal(t, want, status, "%s %s: %s", method, path, data)
		var decoded map[string]json.RawMessage
		if len(data) > 0 {
			require.NoError(t, json.Unmarshal(data, &decoded))
		}
		return decoded
	}
	field := func(raw json.RawMessage, name string) string {
		t.Helper()
		var object map[string]any
		require.NoError(t, json.Unmarshal(raw, &object))
		value, _ := object[name].(string)
		return value
	}

	snapshotID := field(send(http.MethodPost, donorProject+"/snapshots",
		`{"snapshot":{"volume_id":"`+fixtureVolumeID+`"}}`, donor, http.StatusAccepted)["snapshot"], "id")
	send(http.MethodPut, snapshotControlPath(snapshotID, "complete-create"), "", admin, http.StatusNoContent)
	backupID := field(send(http.MethodPost, donorProject+"/backups",
		`{"backup":{"volume_id":"`+fixtureVolumeID+`"}}`, donor, http.StatusAccepted)["backup"], "id")
	send(http.MethodPut, backupControlPath(backupID, "complete-create"), "", admin, http.StatusNoContent)

	for name, tc := range map[string]struct {
		method, path, body, kind, message string
	}{
		"read volume": {http.MethodGet, receiverProject + "/volumes/" + fixtureVolumeID, "",
			"itemNotFound", "The volume could not be found."},
		"read snapshot": {http.MethodGet, receiverProject + "/snapshots/" + snapshotID, "",
			"itemNotFound", "The snapshot could not be found."},
		"read backup": {http.MethodGet, receiverProject + "/backups/" + backupID, "",
			"itemNotFound", "The backup could not be found."},
		"clone volume": {http.MethodPost, receiverProject + "/volumes",
			`{"volume":{"size":1,"source_volid":"` + fixtureVolumeID + `"}}`,
			"itemNotFound", "The source volume could not be found."},
		"snapshot volume": {http.MethodPost, receiverProject + "/snapshots",
			`{"snapshot":{"volume_id":"` + fixtureVolumeID + `"}}`,
			"itemNotFound", "The source volume could not be found."},
		"transfer volume": {http.MethodPost, receiverProject + "/os-volume-transfer",
			`{"transfer":{"volume_id":"` + fixtureVolumeID + `"}}`,
			"itemNotFound", "Volume " + fixtureVolumeID + " could not be found."},
	} {
		t.Run(name, func(t *testing.T) {
			status, _, body := request(t, instance, tc.method, tc.path, tc.body, receiver)

			assert.Equal(t, http.StatusNotFound, status, "%s", body)
			requireFault(t, body, http.StatusNotFound, tc.kind, tc.message)
		})
	}
	assert.JSONEq(t, `[]`, string(send(http.MethodGet, receiverProject+"/volumes/detail", "", receiver, http.StatusOK)["volumes"]))

	created := send(http.MethodPost, donorProject+"/os-volume-transfer",
		`{"transfer":{"volume_id":"`+fixtureVolumeID+`"}}`, donor, http.StatusAccepted)["transfer"]
	transferID, authKey := field(created, "id"), field(created, "auth_key")
	send(http.MethodGet, receiverProject+"/os-volume-transfer/"+transferID, "", receiver, http.StatusNotFound)

	accepted := send(http.MethodPost, receiverProject+"/os-volume-transfer/"+transferID+"/accept",
		`{"accept":{"auth_key":"`+authKey+`"}}`, receiver, http.StatusAccepted)["transfer"]
	moved := send(http.MethodGet, receiverProject+"/volumes/"+fixtureVolumeID, "", receiver, http.StatusOK)["volume"]
	movedSnapshot := send(http.MethodGet, receiverProject+"/snapshots/"+snapshotID, "", receiver, http.StatusOK)["snapshot"]
	send(http.MethodGet, receiverProject+"/backups/"+backupID, "", receiver, http.StatusNotFound)
	send(http.MethodGet, donorProject+"/volumes/"+fixtureVolumeID, "", donor, http.StatusNotFound)
	send(http.MethodGet, donorProject+"/snapshots/"+snapshotID, "", donor, http.StatusNotFound)
	kept := send(http.MethodGet, donorProject+"/backups/"+backupID, "", donor, http.StatusOK)["backup"]
	donorVolumes := send(http.MethodGet, donorProject+"/volumes/detail", "", donor, http.StatusOK)["volumes"]
	upload := `{"os-volume_upload_image":{"image_name":"moved"}}`
	send(http.MethodPost, donorProject+"/volumes/"+fixtureVolumeID+"/action", upload, donor, http.StatusNotFound)
	uploaded := send(http.MethodPost, receiverProject+"/volumes/"+fixtureVolumeID+"/action",
		upload, receiver, http.StatusAccepted)["os-volume_upload_image"]
	afterUpload := send(http.MethodGet, receiverProject+"/volumes/"+fixtureVolumeID, "", receiver, http.StatusOK)["volume"]
	uploadedImage := send(http.MethodGet, imagesPath+"/"+field(uploaded, "image_id"), "", receiver, http.StatusOK)
	donorImageStatus, _, donorImageBody := request(t, instance, http.MethodGet,
		imagesPath+"/"+field(uploaded, "image_id"), "", donor)

	assert.Equal(t, fixtureVolumeID, field(accepted, "volume_id"))
	assert.Equal(t, "available", field(moved, "status"))
	assert.Equal(t, receiverProjectID, field(moved, "os-vol-tenant-attr:tenant_id"))
	assert.Equal(t, fixtureUserID(t, second), field(moved, "user_id"))
	assert.Equal(t, receiverProjectID, field(movedSnapshot, "os-extended-snapshot-attributes:project_id"))
	assert.Equal(t, fixtureVolumeID, field(kept, "volume_id"))
	assert.JSONEq(t, `[]`, string(donorVolumes))
	assert.Equal(t, "available", field(afterUpload, "status"))
	assert.Equal(t, "shared", field(uploaded, "visibility"))
	assert.Equal(t, http.StatusNotFound, donorImageStatus, "%s", donorImageBody)
	assert.JSONEq(t, `"`+receiverProjectID+`"`, string(uploadedImage["owner"]))

	send(http.MethodPost, "/_sandstack/reset", "", admin, http.StatusNoContent)
	donor = map[string]string{"X-Auth-Token": authenticate(t, instance).Token()}
	restored := send(http.MethodGet, donorProject+"/volumes/"+fixtureVolumeID, "", donor, http.StatusOK)["volume"]

	assert.Equal(t, volumeProjectID(t, primary), field(restored, "os-vol-tenant-attr:tenant_id"))
}
