package server_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/volumes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVolumeCreateAndDeleteWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	projectID := volumeProjectID(t, provider)
	collection := volumePath + "/" + projectID + "/volumes"
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	const createBody = `{"volume":{"size":3,"name":"wire-volume","description":"blank volume"}}`

	status, _, body := request(t, instance, http.MethodPost, collection, createBody, nil)
	assert.Equal(t, http.StatusUnauthorized, status, "%s", body)
	for _, invalid := range []struct {
		name string
		body string
	}{
		{"missing size", `{"volume":{"name":"missing"}}`},
		{"zero size", `{"volume":{"size":0}}`},
		{"negative size", `{"volume":{"size":-1}}`},
		{"volume source outside fixture scope", `{"volume":{"size":1,"source_volid":"00000000-0000-0000-0000-000000000000"}}`},
	} {
		t.Run(invalid.name, func(t *testing.T) {
			status, headers, body := request(t, instance, http.MethodPost, collection, invalid.body, auth)
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

	// https://docs.openstack.org/api-ref/block-storage/v3/#create-a-volume
	status, headers, body := request(t, instance, http.MethodPost, collection, createBody, auth)
	require.Equal(t, http.StatusAccepted, status, "%s", body)
	requireJSON(t, headers)
	var response struct {
		Volume struct {
			ID          string          `json:"id"`
			Name        string          `json:"name"`
			Description string          `json:"description"`
			Size        int             `json:"size"`
			Status      string          `json:"status"`
			ProjectID   string          `json:"os-vol-tenant-attr:tenant_id"`
			CreatedAt   string          `json:"created_at"`
			UpdatedAt   json.RawMessage `json:"updated_at"`
		} `json:"volume"`
	}
	require.NoError(t, json.Unmarshal(body, &response))
	created := response.Volume
	require.NotEmpty(t, created.ID)
	assert.NotEqual(t, fixtureVolumeID, created.ID)
	assert.Equal(t, "wire-volume", created.Name)
	assert.Equal(t, "blank volume", created.Description)
	assert.Equal(t, 3, created.Size)
	assert.Equal(t, "creating", created.Status)
	assert.Equal(t, projectID, created.ProjectID)
	assert.NotEmpty(t, created.CreatedAt)
	assert.JSONEq(t, "null", string(created.UpdatedAt))

	status, headers, body = request(t, instance, http.MethodGet, collection+"/detail", "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	var listing struct {
		Volumes []wireVolume `json:"volumes"`
	}
	require.NoError(t, json.Unmarshal(body, &listing))
	require.Len(t, listing.Volumes, 2)
	ids := []string{listing.Volumes[0].ID, listing.Volumes[1].ID}
	assert.ElementsMatch(t, []string{fixtureVolumeID, created.ID}, ids)

	detail := collection + "/" + created.ID
	status, _, body = request(t, instance, http.MethodDelete, detail, "", auth)
	assert.Equal(t, http.StatusBadRequest, status, "%s", body)
	status, _, body = request(t, instance, http.MethodPut, volumeCompleteCreatePath(created.ID), "", map[string]string{
		"X-SandStack-Admin-Token": adminToken,
	})
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	// https://docs.openstack.org/api-ref/block-storage/v3/#delete-a-volume
	status, _, body = request(t, instance, http.MethodDelete, detail, "", auth)
	require.Equal(t, http.StatusAccepted, status, "%s", body)
	assert.Empty(t, body)
	status, _, body = request(t, instance, http.MethodGet, detail, "", auth)
	assert.Equal(t, http.StatusNotFound, status, "%s", body)
}

func TestGophercloudCreatesCompletesAndDeletesVolume(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	discover := func(provider *gophercloud.ProviderClient) *gophercloud.ServiceClient {
		t.Helper()
		result, err := openstack.NewBlockStorageV3(provider, gophercloud.EndpointOpts{
			Region: "RegionOne", Availability: gophercloud.AvailabilityPublic,
		})
		require.NoError(t, err)
		return result
	}
	service := discover(provider)
	list := func(client *gophercloud.ServiceClient) []volumes.Volume {
		t.Helper()
		pages, err := volumes.List(client, nil).AllPages(t.Context())
		require.NoError(t, err)
		result, err := volumes.ExtractVolumes(pages)
		require.NoError(t, err)
		return result
	}
	create := func(opts volumes.CreateOpts) *volumes.Volume {
		t.Helper()
		result, err := volumes.Create(t.Context(), service, opts, nil).Extract()
		require.NoError(t, err)
		require.NotNil(t, result)
		require.NotEmpty(t, result.ID)
		return result
	}
	read := func(id string) *volumes.Volume {
		t.Helper()
		result, err := volumes.Get(t.Context(), service, id).Extract()
		require.NoError(t, err)
		require.NotNil(t, result)
		return result
	}
	baseline := list(service)
	require.Len(t, baseline, 1)

	created := create(volumes.CreateOpts{Size: 4, Name: "sdk-volume", Description: "temporary test disk"})
	assert.NotEqual(t, baseline[0].ID, created.ID)
	assert.Equal(t, 4, created.Size)
	assert.Equal(t, "sdk-volume", created.Name)
	assert.Equal(t, "temporary test disk", created.Description)
	assert.Equal(t, "creating", created.Status)
	assert.Equal(t, volumeProjectID(t, provider), created.TenantID)
	assert.False(t, created.CreatedAt.IsZero())
	assert.True(t, created.UpdatedAt.IsZero())
	assert.Equal(t, created, read(created.ID))
	assert.ElementsMatch(t, []volumes.Volume{baseline[0], *created}, list(service))

	err := volumes.Delete(t.Context(), service, created.ID, nil).ExtractErr()
	require.Error(t, err)
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusBadRequest), "%v", err)
	for _, credentials := range []map[string]string{nil, {"X-SandStack-Admin-Token": provider.Token()}} {
		status, _, body := request(t, instance, http.MethodPut, volumeCompleteCreatePath(created.ID), "", credentials)
		assert.Equal(t, http.StatusUnauthorized, status, "%s", body)
	}
	assert.Equal(t, created, read(created.ID))
	assert.ElementsMatch(t, []volumes.Volume{baseline[0], *created}, list(service))

	admin := map[string]string{"X-SandStack-Admin-Token": adminToken}
	status, _, body := request(t, instance, http.MethodPut, volumeCompleteCreatePath(created.ID), "", admin)
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	available := read(created.ID)
	assert.Equal(t, created.ID, available.ID)
	assert.Equal(t, created.Size, available.Size)
	assert.Equal(t, created.CreatedAt, available.CreatedAt)
	assert.Equal(t, "available", available.Status)
	assert.False(t, available.UpdatedAt.IsZero())
	assert.False(t, available.UpdatedAt.Before(available.CreatedAt))
	assert.ElementsMatch(t, []volumes.Volume{baseline[0], *available}, list(service))
	status, _, body = request(t, instance, http.MethodPut, volumeCompleteCreatePath(created.ID), "", admin)
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	assert.Equal(t, available, read(created.ID))

	fault := "/_sandstack/volume/volumes/" + created.ID + "/fault"
	status, _, body = request(t, instance, http.MethodPut, fault, "", admin)
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	assert.Equal(t, "error", read(created.ID).Status)
	status, _, body = request(t, instance, http.MethodDelete, fault, "", admin)
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	assert.Equal(t, "available", read(created.ID).Status)

	require.NoError(t, volumes.Delete(t.Context(), service, created.ID, nil).ExtractErr())
	_, err = volumes.Get(t.Context(), service, created.ID).Extract()
	require.Error(t, err)
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusNotFound), "%v", err)
	assert.Equal(t, baseline, list(service))

	pending := create(volumes.CreateOpts{Size: 4, Name: "pending-reset-volume"})
	require.Equal(t, "creating", pending.Status)
	status, _, body = request(t, instance, http.MethodPost, "/_sandstack/reset", "", admin)
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	_, err = volumes.Get(t.Context(), service, pending.ID).Extract()
	require.Error(t, err)
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusUnauthorized), "%v", err)
	refreshed := discover(authenticate(t, instance))
	assert.Equal(t, baseline, list(refreshed))
	_, err = volumes.Get(t.Context(), refreshed, pending.ID).Extract()
	require.Error(t, err)
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusNotFound), "%v", err)
}

func volumeCompleteCreatePath(id string) string {
	return "/_sandstack/volume/volumes/" + id + "/complete-create"
}
