package server_test

import (
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

	show := func(id string) map[string]string {
		t.Helper()
		var result struct {
			Metadata map[string]string `json:"metadata"`
		}
		response, err := client.Post(ctx, client.ServiceURL("volumes", id, "action"),
			map[string]any{"os-show_image_metadata": map[string]any{}}, &result,
			&gophercloud.RequestOpts{OkCodes: []int{http.StatusOK}})
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, response.StatusCode)
		requireJSON(t, response.Header)
		return result.Metadata
	}
	assert.Equal(t, map[string]string{}, show(fixtureVolumeID))
	userMetadata := map[string]string{"image_id": "user-owned value", "purpose": "keep"}
	created, err := volumes.Create(ctx, client, volumes.CreateOpts{
		Size: 1, ImageID: fixtureImageID, Metadata: userMetadata,
	}, nil).Extract()
	require.NoError(t, err)
	require.Equal(t, "creating", created.Status)
	assert.Equal(t, map[string]string{}, show(created.ID))

	admin := map[string]string{"X-SandStack-Admin-Token": adminToken}
	status, _, body := request(t, instance, http.MethodPut, volumeCompleteCreatePath(created.ID), "", admin)
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	completed, err := volumes.Get(ctx, client, created.ID).Extract()
	require.NoError(t, err)
	require.Equal(t, fixtureImageID, completed.VolumeImageMetadata["image_id"])
	assert.Equal(t, userMetadata, completed.Metadata)
	assert.Equal(t, completed.VolumeImageMetadata, show(created.ID))

	require.NoError(t, volumes.SetBootable(ctx, client, created.ID, volumes.BootableOpts{
		Bootable: false,
	}).ExtractErr())
	outage := "/_sandstack/volume/volumes/" + created.ID + "/unavailable-detail"
	status, _, body = request(t, instance, http.MethodPut, outage, "", admin)
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	assert.Equal(t, completed.VolumeImageMetadata, show(created.ID))

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
	base := volumePath + "/" + volumeProjectID(t, provider) + "/volumes/"
	action := base + fixtureVolumeID + "/action"
	auth := map[string]string{"X-Auth-Token": provider.Token()}

	// https://docs.openstack.org/api-ref/block-storage/v3/#show-image-metadata-for-a-volume
	for _, payload := range []string{`{"os-show_image_metadata":{}}`, `{"os-show_image_metadata":null}`} {
		status, headers, body := request(t, instance, http.MethodPost, action, payload, auth)

		require.Equal(t, http.StatusOK, status, "%s", body)
		requireJSON(t, headers)
		assert.Equal(t, "volume 3.0", headers.Get("OpenStack-API-Version"))
		assert.JSONEq(t, `{"metadata":{}}`, string(body))
	}

	const valid = `{"os-show_image_metadata":{}}`
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
		{"missing volume", base + "00000000-0000-0000-0000-000000000000/action", valid, auth, http.StatusNotFound},
		{"unauthorized", action, valid, nil, http.StatusUnauthorized},
	} {
		t.Run(tt.name, func(t *testing.T) {
			status, headers, body := request(t, instance, http.MethodPost, tt.path, tt.payload, tt.headers)

			assert.Equal(t, tt.status, status, "%s", body)
			requireJSON(t, headers)
		})
	}
}
