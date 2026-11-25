package server_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/snapshots"
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/volumes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSnapshotCreateAndDeleteWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	projectID := volumeProjectID(t, provider)
	collection := volumePath + "/" + projectID + "/snapshots"
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	const payload = `{"snapshot":{"volume_id":"` + fixtureVolumeID + `","name":"wire-snapshot","description":"blank data","force":false}}`
	status, headers, body := request(t, instance, http.MethodGet, collection+"/detail", "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	assert.JSONEq(t, `{"snapshots":[]}`, string(body))

	status, _, body = request(t, instance, http.MethodPost, collection, payload, nil)
	assert.Equal(t, http.StatusUnauthorized, status, "%s", body)
	for _, invalid := range []struct {
		name   string
		body   string
		status int
	}{
		{"missing volume reference", `{"snapshot":{"name":"missing"}}`, http.StatusBadRequest},
		{"unknown volume", `{"snapshot":{"volume_id":"00000000-0000-0000-0000-000000000000"}}`, http.StatusNotFound},
		{"forced snapshot outside fixture scope", `{"snapshot":{"volume_id":"` + fixtureVolumeID + `","force":true}}`, http.StatusBadRequest},
	} {
		t.Run(invalid.name, func(t *testing.T) {
			status, headers, body := request(t, instance, http.MethodPost, collection, invalid.body, auth)
			assert.Equal(t, invalid.status, status, "%s", body)
			requireJSON(t, headers)
		})
	}
	status, _, body = request(t, instance, http.MethodGet, collection+"/detail", "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	assert.JSONEq(t, `{"snapshots":[]}`, string(body))

	// https://docs.openstack.org/api-ref/block-storage/v3/#create-a-snapshot
	status, headers, body = request(t, instance, http.MethodPost, collection, payload, auth)
	require.Equal(t, http.StatusAccepted, status, "%s", body)
	requireJSON(t, headers)
	var response struct {
		Snapshot wireSnapshot `json:"snapshot"`
	}
	require.NoError(t, json.Unmarshal(body, &response))
	created := response.Snapshot
	require.NotEmpty(t, created.ID)
	assert.NotEqual(t, fixtureVolumeID, created.ID)
	assert.Equal(t, fixtureVolumeID, created.VolumeID)
	assert.Equal(t, "wire-snapshot", created.Name)
	assert.Equal(t, "blank data", created.Description)
	assert.Equal(t, "creating", created.Status)
	assert.Equal(t, 1, created.Size)
	assert.NotEmpty(t, created.CreatedAt)
	assert.JSONEq(t, "null", string(created.UpdatedAt))
	assert.Equal(t, map[string]string{}, created.Metadata)
	assert.Empty(t, created.ProjectID)
	assert.Empty(t, created.Progress)

	detail := collection + "/" + created.ID
	status, _, body = request(t, instance, http.MethodGet, detail, "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	var view struct {
		Snapshot wireSnapshot `json:"snapshot"`
	}
	require.NoError(t, json.Unmarshal(body, &view))
	owner, err := json.Marshal(projectID)
	require.NoError(t, err)
	want := created
	want.ProjectID = json.RawMessage(owner)
	want.Progress = json.RawMessage(`"0%"`)
	assert.Equal(t, want, view.Snapshot)
	status, _, body = request(t, instance, http.MethodDelete, detail, "", auth)
	assert.Equal(t, http.StatusBadRequest, status, "%s", body)
	status, _, body = request(t, instance, http.MethodPut, snapshotControlPath(created.ID, "fail-create"), "", map[string]string{
		"X-SandStack-Admin-Token": adminToken,
	})
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	assert.Empty(t, body)
	// https://docs.openstack.org/api-ref/block-storage/v3/#delete-a-snapshot
	status, _, body = request(t, instance, http.MethodDelete, detail, "", auth)
	require.Equal(t, http.StatusAccepted, status, "%s", body)
	assert.Empty(t, body)
	status, _, body = request(t, instance, http.MethodGet, detail, "", auth)
	assert.Equal(t, http.StatusNotFound, status, "%s", body)
}

func TestGophercloudSnapshotsRequireSourceAndCompleteBeforeCleanup(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	projectID := volumeProjectID(t, provider)
	discover := func(provider *gophercloud.ProviderClient) *gophercloud.ServiceClient {
		t.Helper()
		storage, err := openstack.NewBlockStorageV3(provider, gophercloud.EndpointOpts{
			Region: "RegionOne", Availability: gophercloud.AvailabilityPublic,
		})
		require.NoError(t, err)
		return storage
	}
	storage := discover(provider)
	list := func(client *gophercloud.ServiceClient) []snapshots.Snapshot {
		t.Helper()
		pages, err := snapshots.ListDetail(client, nil).AllPages(t.Context())
		require.NoError(t, err)
		result, err := snapshots.ExtractSnapshots(pages)
		require.NoError(t, err)
		return result
	}
	read := func(id string) *snapshots.Snapshot {
		t.Helper()
		result, err := snapshots.Get(t.Context(), storage, id).Extract()
		require.NoError(t, err)
		require.NotNil(t, result)
		return result
	}
	create := func(volumeID, name string) *snapshots.Snapshot {
		t.Helper()
		result, err := snapshots.Create(t.Context(), storage, snapshots.CreateOpts{
			VolumeID: volumeID, Name: name,
		}).Extract()
		require.NoError(t, err)
		require.NotNil(t, result)
		require.NotEmpty(t, result.ID)
		return result
	}
	pendingView := func(created *snapshots.Snapshot) snapshots.Snapshot {
		view := *created
		view.ProjectID = projectID
		view.Progress = "0%"
		return view
	}
	assertErrorStatus := func(err error, code int) {
		t.Helper()
		require.Error(t, err)
		assert.True(t, gophercloud.ResponseCodeIs(err, code), "%v", err)
	}
	assert.Empty(t, list(storage))
	source, err := volumes.Create(t.Context(), storage, volumes.CreateOpts{Size: 5, Name: "source-volume"}, nil).Extract()
	require.NoError(t, err)
	require.NotNil(t, source)
	require.NotEmpty(t, source.ID)
	_, err = snapshots.Create(t.Context(), storage, snapshots.CreateOpts{VolumeID: source.ID}).Extract()
	assertErrorStatus(err, http.StatusBadRequest)
	assert.Empty(t, list(storage))

	admin := map[string]string{"X-SandStack-Admin-Token": adminToken}
	setOutcome := func(snapshotID, action string, want int) {
		t.Helper()
		status, _, body := request(t, instance, http.MethodPut, snapshotControlPath(snapshotID, action), "", admin)
		require.Equal(t, want, status, "%s", body)
		if want == http.StatusNoContent {
			assert.Empty(t, body)
		}
	}
	status, _, body := request(t, instance, http.MethodPut, volumeCompleteCreatePath(source.ID), "", admin)
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	toComplete := create(source.ID, "completed-snapshot")
	toFail := create(source.ID, "failed-snapshot")
	for _, created := range []*snapshots.Snapshot{toComplete, toFail} {
		assert.Equal(t, source.ID, created.VolumeID)
		assert.Equal(t, source.Size, created.Size)
		assert.Equal(t, "creating", created.Status)
		assert.False(t, created.CreatedAt.IsZero())
		assert.True(t, created.UpdatedAt.IsZero())
		assert.Empty(t, created.ProjectID)
		assert.Empty(t, created.Progress)
		assert.Equal(t, pendingView(created), *read(created.ID))
	}
	assert.ElementsMatch(t, []snapshots.Snapshot{pendingView(toComplete), pendingView(toFail)}, list(storage))
	assertErrorStatus(snapshots.Delete(t.Context(), storage, toComplete.ID).ExtractErr(), http.StatusBadRequest)
	assertErrorStatus(volumes.Delete(t.Context(), storage, source.ID, nil).ExtractErr(), http.StatusBadRequest)
	status, _, body = request(t, instance, http.MethodPut, snapshotControlPath(toComplete.ID, "complete-create"), "", map[string]string{
		"X-SandStack-Admin-Token": provider.Token(),
	})
	assert.Equal(t, http.StatusUnauthorized, status, "%s", body)
	assert.Equal(t, pendingView(toComplete), *read(toComplete.ID))

	setOutcome(toComplete.ID, "complete-create", http.StatusNoContent)
	available := read(toComplete.ID)
	assert.Equal(t, "available", available.Status)
	assert.Equal(t, projectID, available.ProjectID)
	assert.Equal(t, "100%", available.Progress)
	assert.Equal(t, toComplete.CreatedAt, available.CreatedAt)
	assert.False(t, available.UpdatedAt.IsZero())
	setOutcome(toComplete.ID, "complete-create", http.StatusNoContent)
	setOutcome(toComplete.ID, "fail-create", http.StatusConflict)
	assert.Equal(t, available, read(toComplete.ID))

	setOutcome(toFail.ID, "fail-create", http.StatusNoContent)
	faulted := read(toFail.ID)
	assert.Equal(t, "error", faulted.Status)
	assert.Equal(t, projectID, faulted.ProjectID)
	assert.Equal(t, "0%", faulted.Progress)
	assert.Equal(t, toFail.CreatedAt, faulted.CreatedAt)
	assert.False(t, faulted.UpdatedAt.IsZero())
	setOutcome(toFail.ID, "fail-create", http.StatusNoContent)
	setOutcome(toFail.ID, "complete-create", http.StatusConflict)
	assert.Equal(t, faulted, read(toFail.ID))
	assert.ElementsMatch(t, []snapshots.Snapshot{*available, *faulted}, list(storage))

	require.NoError(t, snapshots.Delete(t.Context(), storage, toComplete.ID).ExtractErr())
	_, err = snapshots.Get(t.Context(), storage, toComplete.ID).Extract()
	assertErrorStatus(err, http.StatusNotFound)
	assertErrorStatus(volumes.Delete(t.Context(), storage, source.ID, nil).ExtractErr(), http.StatusBadRequest)
	sourceAfter, err := volumes.Get(t.Context(), storage, source.ID).Extract()
	require.NoError(t, err)
	require.NotNil(t, sourceAfter)
	assert.Equal(t, "available", sourceAfter.Status)
	require.NoError(t, snapshots.Delete(t.Context(), storage, toFail.ID).ExtractErr())
	require.NoError(t, volumes.Delete(t.Context(), storage, source.ID, nil).ExtractErr())
	assert.Empty(t, list(storage))

	pending := create(fixtureVolumeID, "reset-snapshot")
	require.Equal(t, "creating", pending.Status)
	status, _, body = request(t, instance, http.MethodPost, "/_sandstack/reset", "", admin)
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	_, err = snapshots.Get(t.Context(), storage, pending.ID).Extract()
	assertErrorStatus(err, http.StatusUnauthorized)
	current := discover(authenticate(t, instance))
	assert.Empty(t, list(current))
	_, err = snapshots.Get(t.Context(), current, pending.ID).Extract()
	assertErrorStatus(err, http.StatusNotFound)
}

func snapshotControlPath(id, action string) string {
	return "/_sandstack/volume/snapshots/" + id + "/" + action
}

type wireSnapshot struct {
	ID          string            `json:"id"`
	VolumeID    string            `json:"volume_id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Status      string            `json:"status"`
	Size        int               `json:"size"`
	CreatedAt   string            `json:"created_at"`
	UpdatedAt   json.RawMessage   `json:"updated_at"`
	Metadata    map[string]string `json:"metadata"`
	ProjectID   json.RawMessage   `json:"os-extended-snapshot-attributes:project_id"`
	Progress    json.RawMessage   `json:"os-extended-snapshot-attributes:progress"`
}
