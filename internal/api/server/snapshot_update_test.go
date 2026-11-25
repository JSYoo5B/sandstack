package server_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/snapshots"
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/volumes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGophercloudUpdatesCreatingAndAvailableSnapshots(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	storage, err := openstack.NewBlockStorageV3(provider, gophercloud.EndpointOpts{
		Region: "RegionOne", Availability: gophercloud.AvailabilityPublic,
	})
	require.NoError(t, err)
	ctx := t.Context()
	source, err := volumes.Get(ctx, storage, fixtureVolumeID).Extract()
	require.NoError(t, err)
	require.Equal(t, "available", source.Status)
	created, err := snapshots.Create(ctx, storage, snapshots.CreateOpts{
		VolumeID: source.ID, Name: "before-update", Description: "keep-description",
	}).Extract()
	require.NoError(t, err)
	require.NotEmpty(t, created.ID)
	require.Equal(t, "creating", created.Status)
	require.True(t, created.UpdatedAt.IsZero())
	projectID := volumeProjectID(t, provider)
	read := func() *snapshots.Snapshot {
		t.Helper()
		snapshot, err := snapshots.Get(ctx, storage, created.ID).Extract()
		require.NoError(t, err)
		return snapshot
	}
	list := func() []snapshots.Snapshot {
		t.Helper()
		pages, err := snapshots.ListDetail(storage, nil).AllPages(ctx)
		require.NoError(t, err)
		items, err := snapshots.ExtractSnapshots(pages)
		require.NoError(t, err)
		return items
	}
	name := "  renamed-snapshot  "
	revised, err := snapshots.Update(ctx, storage, created.ID, snapshots.UpdateOpts{Name: &name}).Extract()

	require.NoError(t, err)
	want := *created
	want.Name = "renamed-snapshot"
	assert.Equal(t, &want, revised)
	pending := read()
	require.False(t, pending.UpdatedAt.IsZero())
	assert.False(t, pending.UpdatedAt.Before(created.CreatedAt))
	want.UpdatedAt, want.ProjectID, want.Progress = pending.UpdatedAt, projectID, "0%"
	assert.Equal(t, &want, pending)
	assert.Equal(t, []snapshots.Snapshot{want}, list())

	status, _, body := request(t, instance, http.MethodPut, snapshotControlPath(created.ID, "complete-create"), "",
		map[string]string{"X-SandStack-Admin-Token": adminToken})
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	available := read()
	require.Equal(t, "available", available.Status)
	require.Equal(t, "100%", available.Progress)
	description := ""
	revised, err = snapshots.Update(ctx, storage, created.ID, snapshots.UpdateOpts{Description: &description}).Extract()

	require.NoError(t, err)
	want = *available
	want.Description = ""
	want.ProjectID, want.Progress = "", ""
	assert.Equal(t, &want, revised)
	detail := read()
	assert.False(t, detail.UpdatedAt.Before(available.UpdatedAt))
	want.UpdatedAt, want.ProjectID, want.Progress = detail.UpdatedAt, projectID, "100%"
	assert.Equal(t, &want, detail)
	assert.Equal(t, []snapshots.Snapshot{want}, list())
	sourceAfter, err := volumes.Get(ctx, storage, source.ID).Extract()
	require.NoError(t, err)
	assert.Equal(t, source, sourceAfter)
}

func TestSnapshotMetadataUpdateWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	projectID := volumeProjectID(t, provider)
	collection := volumePath + "/" + projectID + "/snapshots"
	auth := map[string]string{"X-Auth-Token": provider.Token(), "OpenStack-API-Version": "volume 3.0"}
	read := func(body []byte) map[string]json.RawMessage {
		t.Helper()
		var response struct {
			Snapshot map[string]json.RawMessage `json:"snapshot"`
		}
		require.NoError(t, json.Unmarshal(body, &response))
		require.Contains(t, response.Snapshot, "id")
		return response.Snapshot
	}
	status, headers, body := request(t, instance, http.MethodPost, collection,
		`{"snapshot":{"volume_id":"`+fixtureVolumeID+`","name":"existing","description":"keep"}}`, auth)
	require.Equal(t, http.StatusAccepted, status, "%s", body)
	requireJSON(t, headers)
	created := read(body)
	var id string
	require.NoError(t, json.Unmarshal(created["id"], &id))
	require.NotEmpty(t, id)
	assert.JSONEq(t, "null", string(created["updated_at"]))
	path := collection + "/" + id
	const changes = `{"snapshot":{"name":null,"description":"   "}}`
	status, _, body = request(t, instance, http.MethodPut, path, changes, nil)
	require.Equal(t, http.StatusUnauthorized, status, "%s", body)

	// https://docs.openstack.org/api-ref/block-storage/v3/#update-a-snapshot
	// https://github.com/openstack/cinder/blob/master/cinder/api/v3/snapshots.py
	status, headers, body = request(t, instance, http.MethodPut, path, changes, auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	assert.Equal(t, "volume 3.0", headers.Get("OpenStack-API-Version"))
	updated := read(body)
	assert.JSONEq(t, "null", string(updated["name"]))
	assert.JSONEq(t, `""`, string(updated["description"]))
	assert.JSONEq(t, "null", string(updated["updated_at"]))
	assert.NotContains(t, updated, "os-extended-snapshot-attributes:project_id")
	assert.NotContains(t, updated, "os-extended-snapshot-attributes:progress")
	for _, field := range []string{"id", "volume_id", "status", "size", "created_at", "metadata"} {
		assert.Equal(t, created[field], updated[field], "%s", field)
	}
	status, headers, body = request(t, instance, http.MethodGet, path, "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	detail := read(body)
	assert.JSONEq(t, "null", string(detail["name"]))
	assert.JSONEq(t, `""`, string(detail["description"]))
	assert.JSONEq(t, `"`+projectID+`"`, string(detail["os-extended-snapshot-attributes:project_id"]))
	assert.JSONEq(t, `"0%"`, string(detail["os-extended-snapshot-attributes:progress"]))
	var changedAt string
	require.NoError(t, json.Unmarshal(detail["updated_at"], &changedAt))
	require.NotEmpty(t, changedAt)
	_, err := time.Parse("2006-01-02T15:04:05.000000", changedAt)
	assert.NoError(t, err)
	expected := string(body)

	for _, invalid := range []string{
		`{}`,
		`{"snapshot":{}}`,
		`{"snapshot":null}`,
		`{"snapshot":{"name":"must-not-change","status":"available"}}`,
		`{"snapshot":{"size":9}}`,
		`{"snapshot":{"description":4}}`,
	} {
		status, headers, body := request(t, instance, http.MethodPut, path, invalid, auth)
		require.Equal(t, http.StatusBadRequest, status, "%s: %s", invalid, body)
		requireJSON(t, headers)
	}
	status, headers, body = request(t, instance, http.MethodGet, path, "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	assert.JSONEq(t, expected, string(body))

	const missing = "00000000-0000-0000-0000-000000000000"
	status, headers, body = request(t, instance, http.MethodPut, collection+"/"+missing, changes, auth)
	require.Equal(t, http.StatusNotFound, status, "%s", body)
	requireJSON(t, headers)
	var fault struct {
		NotFound struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"itemNotFound"`
	}
	require.NoError(t, json.Unmarshal(body, &fault))
	assert.Equal(t, http.StatusNotFound, fault.NotFound.Code)
	assert.NotEmpty(t, fault.NotFound.Message)
}
