package server_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/volumes"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/volumeattach"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVolumeExtensionWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	projectID := volumeProjectID(t, provider)
	collection := volumePath + "/" + projectID + "/volumes"
	detail := collection + "/" + fixtureVolumeID
	action := detail + "/action"
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	const payload = `{"os-extend":{"new_size":3}}`
	const missingID = "00000000-0000-0000-0000-000000000000"
	status, _, body := request(t, instance, http.MethodPost, action, payload, nil)
	assert.Equal(t, http.StatusUnauthorized, status, "%s", body)
	status, _, body = request(t, instance, http.MethodPost, collection+"/"+missingID+"/action", payload, auth)
	assert.Equal(t, http.StatusNotFound, status, "%s", body)
	for _, invalid := range []struct {
		name string
		body string
	}{
		{"same size", `{"os-extend":{"new_size":1}}`},
		{"missing size", `{"os-extend":{}}`},
		{"zero size", `{"os-extend":{"new_size":0}}`},
		{"fractional size", `{"os-extend":{"new_size":1.5}}`},
		{"size above Cinder integer limit", `{"os-extend":{"new_size":2147483648}}`},
		{"duplicate size", `{"os-extend":{"new_size":3,"new_size":4}}`},
		{"extra action", `{"os-extend":{"new_size":3},"os-reset_status":{}}`},
	} {
		t.Run(invalid.name, func(t *testing.T) {
			status, headers, body := request(t, instance, http.MethodPost, action, invalid.body, auth)
			require.Equal(t, http.StatusBadRequest, status, "%s", body)
			requireJSON(t, headers)
			var response struct {
				Error struct {
					Code    int    `json:"code"`
					Message string `json:"message"`
				} `json:"badRequest"`
			}
			require.NoError(t, json.Unmarshal(body, &response))
			assert.Equal(t, http.StatusBadRequest, response.Error.Code)
			assert.NotEmpty(t, response.Error.Message)
		})
	}
	read := func() wireVolume {
		t.Helper()
		status, headers, body := request(t, instance, http.MethodGet, detail, "", auth)
		require.Equal(t, http.StatusOK, status, "%s", body)
		requireJSON(t, headers)
		var response struct {
			Volume wireVolume `json:"volume"`
		}
		require.NoError(t, json.Unmarshal(body, &response))
		return response.Volume
	}
	initial := read()
	assert.Equal(t, "available", initial.Status)
	assert.Equal(t, 1, initial.Size)

	// https://docs.openstack.org/api-ref/block-storage/v3/#extend-a-volume-size
	status, _, body = request(t, instance, http.MethodPost, action, payload, auth)
	require.Equal(t, http.StatusAccepted, status, "%s", body)
	assert.Empty(t, body)
	extending := read()
	assert.Equal(t, initial.ID, extending.ID)
	assert.Equal(t, initial.Size, extending.Size)
	assert.Equal(t, "extending", extending.Status)
	admin := map[string]string{"X-SandStack-Admin-Token": adminToken}
	status, _, body = request(t, instance, http.MethodPut, volumeExtensionControl(fixtureVolumeID, "fail-extend"), "", admin)
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	failed := read()
	assert.Equal(t, "error_extending", failed.Status)
	assert.Equal(t, initial.Size, failed.Size)
	status, _, body = request(t, instance, http.MethodDelete, detail, "", auth)
	require.Equal(t, http.StatusAccepted, status, "%s", body)
	assert.Empty(t, body)
	status, _, body = request(t, instance, http.MethodGet, detail, "", auth)
	assert.Equal(t, http.StatusNotFound, status, "%s", body)
}

func TestGophercloudExtendsVolumesThroughExplicitOutcomes(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	endpoint := gophercloud.EndpointOpts{Region: "RegionOne", Availability: gophercloud.AvailabilityPublic}
	storage, err := openstack.NewBlockStorageV3(provider, endpoint)
	require.NoError(t, err)
	compute, err := openstack.NewComputeV2(provider, endpoint)
	require.NoError(t, err)
	read := func(id string) *volumes.Volume {
		t.Helper()
		result, err := volumes.Get(t.Context(), storage, id).Extract()
		require.NoError(t, err)
		require.NotNil(t, result)
		return result
	}
	list := func(client *gophercloud.ServiceClient) []volumes.Volume {
		t.Helper()
		pages, err := volumes.List(client, nil).AllPages(t.Context())
		require.NoError(t, err)
		result, err := volumes.ExtractVolumes(pages)
		require.NoError(t, err)
		return result
	}
	baseline := list(storage)
	require.Len(t, baseline, 1)
	created, err := volumes.Create(t.Context(), storage, volumes.CreateOpts{Size: 3, Name: "expand-volume"}, nil).Extract()
	require.NoError(t, err)
	require.NotNil(t, created)
	require.NotEmpty(t, created.ID)
	extend := func(size int) error {
		t.Helper()
		return volumes.ExtendSize(t.Context(), storage, created.ID, volumes.ExtendSizeOpts{NewSize: size}).ExtractErr()
	}
	assertBadRequest := func(err error) {
		t.Helper()
		require.Error(t, err)
		assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusBadRequest), "%v", err)
	}
	admin := map[string]string{"X-SandStack-Admin-Token": adminToken}
	control := func(method, path string, want int) {
		t.Helper()
		status, _, body := request(t, instance, method, path, "", admin)
		require.Equal(t, want, status, "%s", body)
	}
	complete := volumeExtensionControl(created.ID, "complete-extend")
	fail := volumeExtensionControl(created.ID, "fail-extend")

	require.Equal(t, "creating", created.Status)
	assertBadRequest(extend(6))
	assert.Equal(t, created, read(created.ID))
	control(http.MethodPut, volumeCompleteCreatePath(created.ID), http.StatusNoContent)
	assertBadRequest(extend(3))
	assertBadRequest(extend(2))
	control(http.MethodPut, complete, http.StatusConflict)
	attached, err := volumeattach.Create(t.Context(), compute, fixtureServerID, volumeattach.CreateOpts{VolumeID: created.ID}).Extract()
	require.NoError(t, err)
	require.NotNil(t, attached)
	require.Equal(t, "in-use", read(created.ID).Status)
	assertBadRequest(extend(6))
	assert.Equal(t, "in-use", read(created.ID).Status)
	require.NoError(t, volumeattach.Delete(t.Context(), compute, fixtureServerID, attached.VolumeID).ExtractErr())
	require.Equal(t, "available", read(created.ID).Status)

	require.NoError(t, extend(6))
	pending := read(created.ID)
	assert.Equal(t, 3, pending.Size)
	assert.Equal(t, "extending", pending.Status)
	assert.Equal(t, created.CreatedAt, pending.CreatedAt)
	assert.ElementsMatch(t, []volumes.Volume{baseline[0], *pending}, list(storage))
	assertBadRequest(extend(8))
	status, _, body := request(t, instance, http.MethodPut, complete, "", map[string]string{
		"X-SandStack-Admin-Token": provider.Token(),
	})
	assert.Equal(t, http.StatusUnauthorized, status, "%s", body)
	assert.Equal(t, pending, read(created.ID))
	control(http.MethodPut, complete, http.StatusNoContent)
	available := read(created.ID)
	assert.Equal(t, "available", available.Status)
	assert.Equal(t, 6, available.Size)
	assert.ElementsMatch(t, []volumes.Volume{baseline[0], *available}, list(storage))
	control(http.MethodPut, complete, http.StatusConflict)
	assert.Equal(t, available, read(created.ID))

	require.NoError(t, extend(9))
	control(http.MethodPut, fail, http.StatusNoContent)
	failed := read(created.ID)
	assert.Equal(t, "error_extending", failed.Status)
	assert.Equal(t, 6, failed.Size)
	assert.ElementsMatch(t, []volumes.Volume{baseline[0], *failed}, list(storage))
	control(http.MethodPut, fail, http.StatusConflict)
	assertBadRequest(extend(10))
	assert.Equal(t, failed, read(created.ID))

	control(http.MethodDelete, "/_sandstack/volume/volumes/"+created.ID+"/fault", http.StatusNoContent)
	recovered := read(created.ID)
	assert.Equal(t, "available", recovered.Status)
	assert.Equal(t, 6, recovered.Size)
	require.NoError(t, extend(10))
	require.Equal(t, "extending", read(created.ID).Status)
	control(http.MethodPost, "/_sandstack/reset", http.StatusNoContent)
	_, err = volumes.Get(t.Context(), storage, created.ID).Extract()
	require.Error(t, err)
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusUnauthorized), "%v", err)
	refreshed, err := openstack.NewBlockStorageV3(authenticate(t, instance), endpoint)
	require.NoError(t, err)
	assert.Equal(t, baseline, list(refreshed))
	control(http.MethodPut, complete, http.StatusNotFound)
}

func volumeExtensionControl(id, action string) string {
	return "/_sandstack/volume/volumes/" + id + "/" + action
}
