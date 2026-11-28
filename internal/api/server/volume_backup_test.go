package server_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/backups"
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/volumes"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/volumeattach"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func backupControlPath(id, action string) string {
	return "/_sandstack/volume/backups/" + id + "/" + action
}

// https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/backup/api.py#L83-L118
// https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/backup/api.py#L218-L451
// https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/backup/manager.py#L585-L768
func TestGophercloudBacksUpAndRestoresVolume(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	endpoint := gophercloud.EndpointOpts{Region: "RegionOne", Availability: gophercloud.AvailabilityPublic}
	storage, err := openstack.NewBlockStorageV3(provider, endpoint)
	require.NoError(t, err)
	compute, err := openstack.NewComputeV2(provider, endpoint)
	require.NoError(t, err)
	ctx := t.Context()
	admin := map[string]string{"X-SandStack-Admin-Token": adminToken}
	control := func(id, action string) {
		t.Helper()
		status, _, body := request(t, instance, http.MethodPut, backupControlPath(id, action), "", admin)
		require.Equal(t, http.StatusNoContent, status, "%s", body)
	}
	readVolume := func(id string) *volumes.Volume {
		t.Helper()
		volume, err := volumes.Get(ctx, storage, id).Extract()
		require.NoError(t, err)
		return volume
	}
	readBackup := func(id string) *backups.Backup {
		t.Helper()
		backup, err := backups.Get(ctx, storage, id).Extract()
		require.NoError(t, err)
		return backup
	}
	setMetadata := func(metadata map[string]string) {
		t.Helper()
		_, err := storage.Put(ctx, storage.ServiceURL("volumes", fixtureVolumeID, "metadata"),
			map[string]any{"metadata": metadata}, nil, &gophercloud.RequestOpts{OkCodes: []int{http.StatusOK}})
		require.NoError(t, err)
	}
	setMetadata(map[string]string{"team": "storage"})

	created, err := backups.Create(ctx, storage, backups.CreateOpts{
		VolumeID: fixtureVolumeID, Name: "nightly", Description: "full copy",
	}).Extract()
	require.NoError(t, err)
	creating := readBackup(created.ID)
	backingUp := readVolume(fixtureVolumeID)
	_, noParentErr := backups.Create(ctx, storage, backups.CreateOpts{VolumeID: fixtureVolumeID, Incremental: true}).Extract()

	assert.Equal(t, "nightly", created.Name)
	assert.Equal(t, "creating", creating.Status)
	assert.Equal(t, "full copy", creating.Description)
	assert.Equal(t, fixtureVolumeID, creating.VolumeID)
	assert.Equal(t, 1, creating.Size)
	assert.False(t, creating.IsIncremental)
	assert.Equal(t, "backing-up", backingUp.Status)
	assert.True(t, gophercloud.ResponseCodeIs(noParentErr, http.StatusBadRequest), "%v", noParentErr)

	control(created.ID, "complete-create")
	full := readBackup(created.ID)
	incremental, err := backups.Create(ctx, storage, backups.CreateOpts{
		VolumeID: fixtureVolumeID, Name: "hourly", Incremental: true,
	}).Extract()
	require.NoError(t, err)
	control(incremental.ID, "complete-create")
	parent := readBackup(created.ID)
	child := readBackup(incremental.ID)
	deleteParentErr := backups.Delete(ctx, storage, created.ID).ExtractErr()
	pages, err := backups.List(storage, backups.ListOpts{VolumeID: fixtureVolumeID, Name: "hourly"}).AllPages(ctx)
	require.NoError(t, err)
	listed, err := backups.ExtractBackups(pages)
	require.NoError(t, err)
	pages, err = backups.ListDetail(storage, backups.ListDetailOpts{Limit: 1}).AllPages(ctx)
	require.NoError(t, err)
	detailed, err := backups.ExtractBackups(pages)
	require.NoError(t, err)

	assert.Equal(t, "available", full.Status)
	assert.Equal(t, "available", readVolume(fixtureVolumeID).Status)
	assert.Equal(t, full.CreatedAt, full.DataTimestamp)
	assert.True(t, parent.HasDependentBackups)
	assert.True(t, child.IsIncremental)
	assert.True(t, gophercloud.ResponseCodeIs(deleteParentErr, http.StatusBadRequest), "%v", deleteParentErr)
	require.Len(t, listed, 1)
	assert.Equal(t, incremental.ID, listed[0].ID)
	assert.Len(t, detailed, 2)

	restore, err := backups.RestoreFromBackup(ctx, storage, created.ID, backups.RestoreOpts{}).Extract()
	require.NoError(t, err)
	restoring := readVolume(restore.VolumeID)
	control(created.ID, "complete-restore")
	restored := readVolume(restore.VolumeID)
	setMetadata(map[string]string{"team": "compute"})
	inPlace, err := backups.RestoreFromBackup(ctx, storage, created.ID, backups.RestoreOpts{
		VolumeID: fixtureVolumeID,
	}).Extract()
	require.NoError(t, err)
	control(created.ID, "complete-restore")

	assert.Equal(t, created.ID, restore.BackupID)
	assert.Equal(t, "restore_backup_"+created.ID, restore.VolumeName)
	assert.Equal(t, "restoring-backup", restoring.Status)
	assert.Equal(t, 1, restoring.Size)
	assert.Equal(t, "available", restored.Status)
	assert.Equal(t, "fixture-volume", restored.Name)
	assert.Equal(t, map[string]string{"team": "storage", "src_backup_id": created.ID}, restored.Metadata)
	assert.Equal(t, fixtureVolumeID, inPlace.VolumeID)
	assert.Equal(t, map[string]string{"team": "storage"}, readVolume(fixtureVolumeID).Metadata)

	_, err = volumeattach.Create(ctx, compute, fixtureServerID, volumeattach.CreateOpts{VolumeID: fixtureVolumeID}).Extract()
	require.NoError(t, err)
	_, unforcedErr := backups.Create(ctx, storage, backups.CreateOpts{VolumeID: fixtureVolumeID}).Extract()
	forced, err := backups.Create(ctx, storage, backups.CreateOpts{VolumeID: fixtureVolumeID, Force: true}).Extract()
	require.NoError(t, err)
	attachedBackingUp := readVolume(fixtureVolumeID)
	control(forced.ID, "fail-create")
	failed := readBackup(forced.ID)
	attached := readVolume(fixtureVolumeID)

	assert.True(t, gophercloud.ResponseCodeIs(unforcedErr, http.StatusBadRequest), "%v", unforcedErr)
	assert.Equal(t, "backing-up", attachedBackingUp.Status)
	assert.Equal(t, "error", failed.Status)
	assert.NotEmpty(t, failed.FailReason)
	assert.Equal(t, "in-use", attached.Status)
	assert.Len(t, attached.Attachments, 1)

	for _, id := range []string{forced.ID, incremental.ID, created.ID} {
		require.NoError(t, backups.Delete(ctx, storage, id).ExtractErr())
	}
	pages, err = backups.ListDetail(storage, nil).AllPages(ctx)
	require.NoError(t, err)
	remaining, err := backups.ExtractBackups(pages)
	require.NoError(t, err)
	assert.Empty(t, remaining)
}

type wireBackup struct {
	ID                  string       `json:"id"`
	Status              string       `json:"status"`
	Size                int          `json:"size"`
	ObjectCount         int          `json:"object_count"`
	AvailabilityZone    *string      `json:"availability_zone"`
	Container           *string      `json:"container"`
	CreatedAt           string       `json:"created_at"`
	UpdatedAt           *string      `json:"updated_at"`
	Name                *string      `json:"name"`
	Description         *string      `json:"description"`
	FailReason          *string      `json:"fail_reason"`
	VolumeID            string       `json:"volume_id"`
	Links               []volumeLink `json:"links"`
	IsIncremental       bool         `json:"is_incremental"`
	HasDependentBackups bool         `json:"has_dependent_backups"`
	SnapshotID          *string      `json:"snapshot_id"`
	DataTimestamp       string       `json:"data_timestamp"`
}

// https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/api/contrib/backups.py#L62-L237
// https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/api/schemas/backups.py#L25-L56
// https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/api/schemas/backups.py#L80-L95
// https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/api/views/backups.py#L37-L82
func TestCinderBackupWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	projectID := volumeProjectID(t, provider)
	project := volumePath + "/" + projectID
	collection := project + "/backups"
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	admin := map[string]string{"X-SandStack-Admin-Token": adminToken}
	readBackup := func(id string) wireBackup {
		t.Helper()
		status, headers, body := request(t, instance, http.MethodGet, collection+"/"+id, "", auth)
		require.Equal(t, http.StatusOK, status, "%s", body)
		requireJSON(t, headers)
		var response struct {
			Backup wireBackup `json:"backup"`
		}
		require.NoError(t, json.Unmarshal(body, &response))
		return response.Backup
	}
	readVolumeStatus := func(id string) string {
		t.Helper()
		status, _, body := request(t, instance, http.MethodGet, project+"/volumes/"+id, "", auth)
		require.Equal(t, http.StatusOK, status, "%s", body)
		var response struct {
			Volume wireVolume `json:"volume"`
		}
		require.NoError(t, json.Unmarshal(body, &response))
		return response.Volume.Status
	}
	status, _, body := request(t, instance, http.MethodGet, collection, "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	assert.JSONEq(t, `{"backups":[]}`, string(body))

	status, _, body = request(t, instance, http.MethodPost, collection,
		`{"backup":{"volume_id":"`+fixtureVolumeID+`"}}`, nil)
	assert.Equal(t, http.StatusUnauthorized, status, "%s", body)
	for _, tc := range []struct {
		name   string
		body   string
		status int
	}{
		{"missing volume", `{"backup":{"name":"missing"}}`, http.StatusBadRequest},
		{"null backup", `{"backup":null}`, http.StatusBadRequest},
		{"string force", `{"backup":{"volume_id":"` + fixtureVolumeID + `","force":"maybe"}}`, http.StatusBadRequest},
		{"metadata before 3.43", `{"backup":{"volume_id":"` + fixtureVolumeID + `","metadata":{}}}`, http.StatusBadRequest},
		{"zone before 3.51", `{"backup":{"volume_id":"` + fixtureVolumeID + `","availability_zone":"nova"}}`, http.StatusBadRequest},
		{"invalid snapshot", `{"backup":{"volume_id":"` + fixtureVolumeID + `","snapshot_id":"snap"}}`, http.StatusBadRequest},
		{"incremental without parent", `{"backup":{"volume_id":"` + fixtureVolumeID + `","incremental":true}}`, http.StatusBadRequest},
		{"unknown volume", `{"backup":{"volume_id":"` + missingVolumeMarker + `"}}`, http.StatusNotFound},
		{"unknown snapshot", `{"backup":{"volume_id":"` + fixtureVolumeID + `","snapshot_id":"` + missingVolumeMarker + `"}}`, http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, headers, body := request(t, instance, http.MethodPost, collection, tc.body, auth)

			assert.Equal(t, tc.status, status, "%s", body)
			requireJSON(t, headers)
		})
	}
	assert.Equal(t, "available", readVolumeStatus(fixtureVolumeID))

	status, headers, body := request(t, instance, http.MethodPost, collection,
		`{"backup":{"volume_id":"`+fixtureVolumeID+`","name":"  wire  ","container":"bucket","force":"false"}}`, auth)
	require.Equal(t, http.StatusAccepted, status, "%s", body)
	requireJSON(t, headers)
	var created struct {
		Backup struct {
			ID    string       `json:"id"`
			Name  *string      `json:"name"`
			Links []volumeLink `json:"links"`
		} `json:"backup"`
	}
	require.NoError(t, json.Unmarshal(body, &created))
	var createdFields map[string]map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(body, &createdFields))
	backupID := created.Backup.ID
	resource := "/" + projectID + "/backups/" + backupID

	assert.Len(t, createdFields["backup"], 3)
	require.NotNil(t, created.Backup.Name)
	assert.Equal(t, "wire", *created.Backup.Name)
	assert.Equal(t, []volumeLink{
		{Rel: "self", Href: instance.URL + volumePath + resource},
		{Rel: "bookmark", Href: instance.URL + "/volume" + resource},
	}, created.Backup.Links)
	assert.Equal(t, "backing-up", readVolumeStatus(fixtureVolumeID))

	pending := readBackup(backupID)
	status, _, body = request(t, instance, http.MethodDelete, collection+"/"+backupID, "", auth)
	assert.Equal(t, http.StatusBadRequest, status, "%s", body)
	status, _, body = request(t, instance, http.MethodPost, collection+"/"+backupID+"/restore", `{"restore":{}}`, auth)
	assert.Equal(t, http.StatusBadRequest, status, "%s", body)
	status, _, body = request(t, instance, http.MethodPut, backupControlPath(backupID, "complete-restore"), "", admin)
	assert.Equal(t, http.StatusConflict, status, "%s", body)
	status, _, body = request(t, instance, http.MethodPut, backupControlPath(backupID, "complete-create"), "", admin)
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	status, _, body = request(t, instance, http.MethodPut, backupControlPath(backupID, "fail-create"), "", admin)
	assert.Equal(t, http.StatusConflict, status, "%s", body)
	status, _, body = request(t, instance, http.MethodPut, backupControlPath(missingVolumeMarker, "complete-create"), "", admin)
	assert.Equal(t, http.StatusNotFound, status, "%s", body)
	available := readBackup(backupID)

	assert.Equal(t, "creating", pending.Status)
	assert.Equal(t, "available", available.Status)
	assert.Equal(t, 1, available.Size)
	assert.Zero(t, available.ObjectCount)
	require.NotNil(t, available.AvailabilityZone)
	assert.Equal(t, "nova", *available.AvailabilityZone)
	require.NotNil(t, available.Container)
	assert.Equal(t, "bucket", *available.Container)
	assert.Nil(t, available.Description)
	assert.Nil(t, available.FailReason)
	assert.Nil(t, available.SnapshotID)
	assert.Equal(t, fixtureVolumeID, available.VolumeID)
	assert.Equal(t, available.CreatedAt, available.DataTimestamp)
	assert.Equal(t, created.Backup.Links, available.Links)
	assert.False(t, available.IsIncremental)
	assert.False(t, available.HasDependentBackups)
	assert.Equal(t, "available", readVolumeStatus(fixtureVolumeID))

	status, _, body = request(t, instance, http.MethodPost, project+"/volumes", `{"volume":{"size":1}}`, auth)
	require.Equal(t, http.StatusAccepted, status, "%s", body)
	var target struct {
		Volume wireVolume `json:"volume"`
	}
	require.NoError(t, json.Unmarshal(body, &target))
	restorePath := collection + "/" + backupID + "/restore"
	for _, tc := range []struct {
		name   string
		path   string
		body   string
		status int
	}{
		{"missing envelope", restorePath, `{}`, http.StatusBadRequest},
		{"extra field", restorePath, `{"restore":{"size":1}}`, http.StatusBadRequest},
		{"invalid volume", restorePath, `{"restore":{"volume_id":"volume"}}`, http.StatusBadRequest},
		{"creating volume", restorePath, `{"restore":{"volume_id":"` + target.Volume.ID + `"}}`, http.StatusBadRequest},
		{"unknown volume", restorePath, `{"restore":{"volume_id":"` + missingVolumeMarker + `"}}`, http.StatusNotFound},
		{"unknown backup", collection + "/" + missingVolumeMarker + "/restore", `{"restore":{}}`, http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, headers, body := request(t, instance, http.MethodPost, tc.path, tc.body, auth)

			assert.Equal(t, tc.status, status, "%s", body)
			requireJSON(t, headers)
		})
	}

	status, headers, body = request(t, instance, http.MethodPost, restorePath,
		`{"restore":{"name":"restored","volume_id":null}}`, auth)
	require.Equal(t, http.StatusAccepted, status, "%s", body)
	requireJSON(t, headers)
	var restore struct {
		Restore map[string]any `json:"restore"`
	}
	require.NoError(t, json.Unmarshal(body, &restore))
	newVolumeID, _ := restore.Restore["volume_id"].(string)

	assert.Equal(t, map[string]any{
		"backup_id": backupID, "volume_id": newVolumeID, "volume_name": "restored",
	}, restore.Restore)
	assert.Equal(t, "restoring", readBackup(backupID).Status)
	assert.Equal(t, "restoring-backup", readVolumeStatus(newVolumeID))

	status, _, body = request(t, instance, http.MethodPut, backupControlPath(backupID, "fail-restore"), "", admin)
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	assert.Equal(t, "error_restoring", readVolumeStatus(newVolumeID))
	assert.Equal(t, "available", readBackup(backupID).Status)
	status, _, body = request(t, instance, http.MethodDelete, project+"/volumes/"+newVolumeID, "", auth)
	assert.Equal(t, http.StatusAccepted, status, "%s", body)
	status, _, body = request(t, instance, http.MethodDelete, project+"/volumes/"+fixtureVolumeID, "", auth)
	assert.Equal(t, http.StatusAccepted, status, "%s", body)
	assert.Equal(t, fixtureVolumeID, readBackup(backupID).VolumeID)

	for _, query := range []string{"?status=available", "?name=wire", "?volume_id=" + fixtureVolumeID, "?sort=id:asc&limit=1"} {
		t.Run("list"+query, func(t *testing.T) {
			status, _, body := request(t, instance, http.MethodGet, collection+query, "", auth)
			require.Equal(t, http.StatusOK, status, "%s", body)
			var response struct {
				Backups []map[string]json.RawMessage `json:"backups"`
			}
			require.NoError(t, json.Unmarshal(body, &response))

			require.Len(t, response.Backups, 1)
			assert.Len(t, response.Backups[0], 3)
		})
	}
	status, _, body = request(t, instance, http.MethodGet, collection+"/detail?limit=1", "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	var page struct {
		Backups []wireBackup `json:"backups"`
		Links   []volumeLink `json:"backups_links"`
	}
	require.NoError(t, json.Unmarshal(body, &page))
	assert.Equal(t, []wireBackup{readBackup(backupID)}, page.Backups)
	assert.Equal(t, []volumeLink{{
		Rel: "next", Href: instance.URL + collection + "/detail?limit=1&marker=" + backupID,
	}}, page.Links)
	for _, query := range []string{"?status=creating", "?volume_id=" + missingVolumeMarker} {
		status, _, body = request(t, instance, http.MethodGet, collection+"/detail"+query, "", auth)
		require.Equal(t, http.StatusOK, status, "%s", body)
		assert.JSONEq(t, `{"backups":[]}`, string(body))
	}
	for _, query := range []string{"?all_tenants=1", "?volume_id=a&volume_id=b", "?sort=created_at:desc"} {
		status, _, body = request(t, instance, http.MethodGet, collection+query, "", auth)
		assert.Equal(t, http.StatusBadRequest, status, "%s", body)
	}
	status, _, body = request(t, instance, http.MethodGet, collection+"?marker="+missingVolumeMarker, "", auth)
	assert.Equal(t, http.StatusNotFound, status, "%s", body)

	status, headers, body = request(t, instance, http.MethodDelete, collection+"/"+backupID, "", auth)
	require.Equal(t, http.StatusAccepted, status, "%s", body)
	assert.Empty(t, body)
	assert.Equal(t, "no-store", headers.Get("Cache-Control"))
	status, _, body = request(t, instance, http.MethodGet, collection+"/"+backupID, "", auth)
	assert.Equal(t, http.StatusNotFound, status, "%s", body)

	status, _, body = request(t, instance, http.MethodPost, collection,
		`{"backup":{"volume_id":"`+target.Volume.ID+`"}}`, auth)
	assert.Equal(t, http.StatusBadRequest, status, "%s", body)
	status, _, body = request(t, instance, http.MethodPut, "/_sandstack/volume/volumes/"+target.Volume.ID+"/complete-create", "", admin)
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	status, _, body = request(t, instance, http.MethodPost, collection,
		`{"backup":{"volume_id":"`+target.Volume.ID+`"}}`, auth)
	require.Equal(t, http.StatusAccepted, status, "%s", body)

	status, _, body = request(t, instance, http.MethodPost, "/_sandstack/reset", "", admin)
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	auth["X-Auth-Token"] = authenticate(t, instance).Token()
	status, _, body = request(t, instance, http.MethodGet, collection+"/detail", "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	assert.JSONEq(t, `{"backups":[]}`, string(body))
	assert.Equal(t, "available", readVolumeStatus(fixtureVolumeID))
}
