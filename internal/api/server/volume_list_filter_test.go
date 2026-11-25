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

func TestGophercloudFiltersDetailedVolumesAcrossChanges(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	endpoint := gophercloud.EndpointOpts{Region: "RegionOne", Availability: gophercloud.AvailabilityPublic}
	client, err := openstack.NewBlockStorageV3(provider, endpoint)
	require.NoError(t, err)
	compute, err := openstack.NewComputeV2(provider, endpoint)
	require.NoError(t, err)
	ctx := t.Context()
	initial, err := volumes.Get(ctx, client, fixtureVolumeID).Extract()
	require.NoError(t, err)
	require.Equal(t, "available", initial.Status)
	create := func(name string) *volumes.Volume {
		t.Helper()
		volume, err := volumes.Create(ctx, client, volumes.CreateOpts{Name: name, Size: 1}, nil).Extract()
		require.NoError(t, err)
		require.NotEmpty(t, volume.ID)
		require.Equal(t, "creating", volume.Status)
		return volume
	}
	sameName := create(initial.Name)
	other := create("other-volume")
	status, _, body := request(t, instance, http.MethodPut, volumeCompleteCreatePath(other.ID), "",
		map[string]string{"X-SandStack-Admin-Token": adminToken})
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	list := func(opts volumes.ListOpts) []volumes.Volume {
		t.Helper()
		pages, err := volumes.List(client, opts).AllPages(ctx)
		require.NoError(t, err)
		items, err := volumes.ExtractVolumes(pages)
		require.NoError(t, err)
		return items
	}
	ids := func(items []volumes.Volume) []string {
		result := make([]string, 0, len(items))
		for _, volume := range items {
			result = append(result, volume.ID)
		}
		return result
	}
	nameOnly := volumes.ListOpts{Name: initial.Name}
	available := volumes.ListOpts{Status: "available"}
	both := volumes.ListOpts{Name: initial.Name, Status: "available"}
	assert.ElementsMatch(t, []string{initial.ID, sameName.ID, other.ID}, ids(list(volumes.ListOpts{})))
	assert.ElementsMatch(t, []string{initial.ID, sameName.ID}, ids(list(nameOnly)))
	assert.ElementsMatch(t, []string{initial.ID, other.ID}, ids(list(available)))
	assert.Equal(t, []volumes.Volume{*initial}, list(both))
	assert.Empty(t, list(volumes.ListOpts{Name: "fixture"}))

	newName := "renamed-filter-volume"
	renamed, err := volumes.Update(ctx, client, initial.ID, volumes.UpdateOpts{Name: &newName}).Extract()
	require.NoError(t, err)
	assert.Equal(t, []string{sameName.ID}, ids(list(nameOnly)))
	assert.Empty(t, list(both))
	both.Name = newName
	assert.Equal(t, []volumes.Volume{*renamed}, list(both))

	_, err = volumeattach.Create(ctx, compute, fixtureServerID, volumeattach.CreateOpts{VolumeID: initial.ID}).Extract()
	require.NoError(t, err)
	assert.Equal(t, []string{other.ID}, ids(list(available)))
	assert.Empty(t, list(both))
	inUse := list(volumes.ListOpts{Name: newName, Status: "in-use"})
	require.Len(t, inUse, 1)
	assert.Equal(t, initial.ID, inUse[0].ID)
	assert.Equal(t, "in-use", inUse[0].Status)
	require.Len(t, inUse[0].Attachments, 1)
	assert.Equal(t, fixtureServerID, inUse[0].Attachments[0].ServerID)

	require.NoError(t, volumeattach.Delete(ctx, compute, fixtureServerID, initial.ID).ExtractErr())
	assert.Empty(t, list(volumes.ListOpts{Name: newName, Status: "in-use"}))
	assert.Equal(t, []string{initial.ID}, ids(list(both)))
}

func TestDetailedVolumeFiltersWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	collection := volumePath + "/" + volumeProjectID(t, provider) + "/volumes"
	detailList := collection + "/detail"
	auth := map[string]string{"X-Auth-Token": provider.Token(), "OpenStack-API-Version": "volume 3.0"}
	type wireItem struct {
		ID          string          `json:"id"`
		Name        json.RawMessage `json:"name"`
		Status      string          `json:"status"`
		Size        int             `json:"size"`
		Attachments json.RawMessage `json:"attachments"`
	}
	create := func(payload string) wireItem {
		t.Helper()
		status, headers, body := request(t, instance, http.MethodPost, collection, payload, auth)
		require.Equal(t, http.StatusAccepted, status, "%s", body)
		requireJSON(t, headers)
		var response struct {
			Volume wireItem `json:"volume"`
		}
		require.NoError(t, json.Unmarshal(body, &response))
		require.NotEmpty(t, response.Volume.ID)
		return response.Volume
	}
	explicit := create(`{"volume":{"size":1,"name":"will-clear"}}`)
	null := create(`{"volume":{"size":1,"name":null}}`)
	omitted := create(`{"volume":{"size":1}}`)
	assert.JSONEq(t, "null", string(null.Name))
	assert.JSONEq(t, "null", string(omitted.Name))
	status, headers, body := request(t, instance, http.MethodPut, collection+"/"+explicit.ID,
		`{"volume":{"name":""}}`, auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)

	// https://docs.openstack.org/api-ref/block-storage/v3/#list-accessible-volumes-with-details
	status, headers, body = request(t, instance, http.MethodGet, detailList+"?status=creating&name=", "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	assert.Equal(t, "volume 3.0", headers.Get("OpenStack-API-Version"))
	var response struct {
		Volumes []wireItem `json:"volumes"`
	}
	require.NoError(t, json.Unmarshal(body, &response))
	require.Len(t, response.Volumes, 1)
	assert.Equal(t, explicit.ID, response.Volumes[0].ID)
	assert.JSONEq(t, `""`, string(response.Volumes[0].Name))
	assert.Equal(t, "creating", response.Volumes[0].Status)
	assert.Equal(t, 1, response.Volumes[0].Size)
	assert.JSONEq(t, "[]", string(response.Volumes[0].Attachments))

	status, headers, body = request(t, instance, http.MethodGet, detailList+"?name=&status=available", "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	assert.JSONEq(t, `{"volumes":[]}`, string(body))
	for _, nonmatch := range []string{"status=", "status=AVAILABLE", "status=unmodeled"} {
		status, headers, body := request(t, instance, http.MethodGet, detailList+"?"+nonmatch, "", auth)
		require.Equal(t, http.StatusOK, status, "%s: %s", nonmatch, body)
		requireJSON(t, headers)
		assert.JSONEq(t, `{"volumes":[]}`, string(body))
	}
	status, _, body = request(t, instance, http.MethodGet, detailList+"?status=available", "", nil)
	require.Equal(t, http.StatusUnauthorized, status, "%s", body)

	for _, invalid := range []string{
		"name=fixture-volume&name=another",
		"status=available&status=creating",
		"name=%zz",
		"limit=1",
	} {
		status, headers, body := request(t, instance, http.MethodGet, detailList+"?"+invalid, "", auth)
		require.Equal(t, http.StatusBadRequest, status, "%s: %s", invalid, body)
		requireJSON(t, headers)
		var fault struct {
			BadRequest struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"badRequest"`
		}
		require.NoError(t, json.Unmarshal(body, &fault))
		assert.Equal(t, http.StatusBadRequest, fault.BadRequest.Code)
		assert.NotEmpty(t, fault.BadRequest.Message)
	}
}
