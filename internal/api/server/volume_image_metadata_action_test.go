package server_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/volumes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGophercloudReadsCinderVolumeImageMetadataAction(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	client, err := openstack.NewBlockStorageV3(provider, gophercloud.EndpointOpts{
		Region:       "RegionOne",
		Availability: gophercloud.AvailabilityPublic,
	})
	require.NoError(t, err)
	ctx := t.Context()

	show := func(id string, wantStatus int) map[string]string {
		t.Helper()
		var result struct {
			Metadata map[string]string `json:"metadata"`
		}
		response, err := client.Post(ctx, client.ServiceURL("volumes", id, "action"),
			map[string]any{"os-show_image_metadata": map[string]any{}}, &result,
			&gophercloud.RequestOpts{OkCodes: []int{http.StatusOK}})
		if wantStatus == http.StatusNotFound {
			require.True(t, gophercloud.ResponseCodeIs(err, wantStatus), "%v", err)
			return nil
		}
		require.NoError(t, err)
		require.Equal(t, wantStatus, response.StatusCode)
		requireJSON(t, response.Header)
		return result.Metadata
	}
	show(fixtureVolumeID, http.StatusNotFound)
	userMetadata := map[string]string{"image_id": "user-owned value", "purpose": "keep"}
	created, err := volumes.Create(ctx, client, volumes.CreateOpts{
		Size: 1, ImageID: fixtureImageID, Metadata: userMetadata,
	}, nil).Extract()
	require.NoError(t, err)
	require.Equal(t, "creating", created.Status)
	show(created.ID, http.StatusNotFound)

	admin := map[string]string{"X-SandStack-Admin-Token": adminToken}
	status, _, body := request(t, instance, http.MethodPut, volumeCompleteCreatePath(created.ID), "", admin)
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	completed, err := volumes.Get(ctx, client, created.ID).Extract()
	require.NoError(t, err)
	require.Equal(t, fixtureImageID, completed.VolumeImageMetadata["image_id"])
	assert.Equal(t, userMetadata, completed.Metadata)
	assert.Equal(t, completed.VolumeImageMetadata, show(created.ID, http.StatusOK))

	require.NoError(t, volumes.SetBootable(ctx, client, created.ID, volumes.BootableOpts{
		Bootable: false,
	}).ExtractErr())
	outage := "/_sandstack/volume/volumes/" + created.ID + "/unavailable-detail"
	status, _, body = request(t, instance, http.MethodPut, outage, "", admin)
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	assert.Equal(t, completed.VolumeImageMetadata, show(created.ID, http.StatusOK))

	_, err = volumes.Get(ctx, client, created.ID).Extract()
	require.True(t, gophercloud.ResponseCodeIs(err, http.StatusServiceUnavailable), "%v", err)
	current, err := volumes.Get(ctx, client, created.ID).Extract()
	require.NoError(t, err)
	assert.Equal(t, "false", current.Bootable)
	assert.Equal(t, completed.VolumeImageMetadata, current.VolumeImageMetadata)
	assert.Equal(t, userMetadata, current.Metadata)
}

func TestCinderVolumeImageMetadataActionWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	base := volumePath + "/" + volumeProjectID(t, provider) + "/volumes"
	action := base + "/" + fixtureVolumeID + "/action"
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	admin := map[string]string{"X-SandStack-Admin-Token": adminToken}
	create := fmt.Sprintf(`{"volume":{"size":1,"imageRef":%q}}`, fixtureImageID)
	status, _, body := request(t, instance, http.MethodPost, base, create, auth)
	require.Equal(t, http.StatusAccepted, status, "%s", body)
	var created struct {
		Volume wireVolume `json:"volume"`
	}
	require.NoError(t, json.Unmarshal(body, &created))
	require.NotEmpty(t, created.Volume.ID)
	readyAction := base + "/" + created.Volume.ID + "/action"
	status, _, body = request(t, instance, http.MethodPut, volumeCompleteCreatePath(created.Volume.ID), "", admin)
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	want, err := json.Marshal(map[string]map[string]string{"metadata": {
		"image_id": fixtureImageID, "image_name": "fixture-image", "container_format": "bare", "disk_format": "qcow2",
		"checksum": fixtureImageMD5, "size": fmt.Sprint(fixtureImageDataSize), "min_disk": "0", "min_ram": "0",
	}})
	require.NoError(t, err)

	// https://docs.openstack.org/api-ref/block-storage/v3/#show-image-metadata-for-a-volume
	for _, payload := range []string{`{"os-show_image_metadata":{}}`, `{"os-show_image_metadata":null}`} {
		status, headers, body := request(t, instance, http.MethodPost, readyAction, payload, auth)

		require.Equal(t, http.StatusOK, status, "%s", body)
		requireJSON(t, headers)
		assert.Equal(t, "volume 3.0", headers.Get("OpenStack-API-Version"))
		assert.JSONEq(t, string(want), string(body))
	}

	const valid = `{"os-show_image_metadata":{}}`
	status, headers, body := request(t, instance, http.MethodPost, action, valid, auth)
	require.Equal(t, http.StatusNotFound, status, "%s", body)
	requireJSON(t, headers)
	var missing map[string]struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	require.NoError(t, json.Unmarshal(body, &missing))
	require.Len(t, missing, 1)
	require.Contains(t, missing, "itemNotFound")
	assert.Equal(t, http.StatusNotFound, missing["itemNotFound"].Code)
	assert.NotEmpty(t, missing["itemNotFound"].Message)
	for _, tt := range []struct {
		name    string
		path    string
		payload string
		headers map[string]string
		status  int
	}{
		{"missing action", action, `{}`, auth, http.StatusBadRequest},
		{"malformed JSON", action, `{"os-show_image_metadata":`, auth, http.StatusBadRequest},
		{"duplicate action", action, `{"os-show_image_metadata":{},"os-show_image_metadata":{}}`, auth, http.StatusBadRequest},
		{"missing volume", base + "/00000000-0000-0000-0000-000000000000/action", valid, auth, http.StatusNotFound},
		{"unauthorized", action, valid, nil, http.StatusUnauthorized},
	} {
		t.Run(tt.name, func(t *testing.T) {
			status, headers, body := request(t, instance, http.MethodPost, tt.path, tt.payload, tt.headers)

			assert.Equal(t, tt.status, status, "%s", body)
			requireJSON(t, headers)
		})
	}
}
