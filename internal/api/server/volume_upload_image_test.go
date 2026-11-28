package server_test

import (
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"slices"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/volumes"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/volumeattach"
	"github.com/gophercloud/gophercloud/v2/openstack/image/v2/imagedata"
	"github.com/gophercloud/gophercloud/v2/openstack/image/v2/images"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/api/contrib/volume_actions.py#L199-L269
// https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/volume/api.py#L1483-L1552
// https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/volume/manager.py#L1752-L1803
func TestGophercloudUploadsCinderVolumeToGlanceImage(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	endpoint := gophercloud.EndpointOpts{Region: "RegionOne", Availability: gophercloud.AvailabilityPublic}
	storage, err := openstack.NewBlockStorageV3(provider, endpoint)
	require.NoError(t, err)
	imageClient, err := openstack.NewImageV2(provider, endpoint)
	require.NoError(t, err)
	compute, err := openstack.NewComputeV2(provider, endpoint)
	require.NoError(t, err)
	ctx := t.Context()
	admin := map[string]string{"X-SandStack-Admin-Token": adminToken}
	source, err := images.Get(ctx, imageClient, fixtureImageID).Extract()
	require.NoError(t, err)
	require.NotZero(t, source.SizeBytes)
	readData := func(imageID string) string {
		t.Helper()
		download, err := imagedata.Download(ctx, imageClient, imageID).Extract()
		require.NoError(t, err)
		data, err := io.ReadAll(download)
		require.NoError(t, err)
		require.NoError(t, download.Close())
		return string(data)
	}
	created, err := volumes.Create(ctx, storage, volumes.CreateOpts{
		Size: 1, ImageID: fixtureImageID, Description: "boot disk",
	}, nil).Extract()
	require.NoError(t, err)
	status, _, body := request(t, instance, http.MethodPut,
		"/_sandstack/volume/volumes/"+created.ID+"/complete-create", "", admin)
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	before, err := volumes.Get(ctx, storage, created.ID).Extract()
	require.NoError(t, err)

	uploaded, err := volumes.UploadImage(ctx, storage, created.ID, volumes.UploadImageOpts{
		ImageName: "volume upload",
	}).Extract()

	require.NoError(t, err)
	assert.Equal(t, created.ID, uploaded.VolumeID)
	assert.Equal(t, "uploading", uploaded.Status)
	assert.Equal(t, "volume upload", uploaded.ImageName)
	assert.Equal(t, "bare", uploaded.ContainerFormat)
	assert.Equal(t, "raw", uploaded.DiskFormat)
	assert.Equal(t, "boot disk", uploaded.Description)
	assert.Equal(t, 1, uploaded.Size)
	assert.Equal(t, before.UpdatedAt, uploaded.UpdatedAt)
	assert.Equal(t, fixtureVolumeTypeID, uploaded.VolumeType.ID)
	assert.Equal(t, "standard", uploaded.VolumeType.Name)
	assert.True(t, uploaded.VolumeType.IsPublic)
	assert.Equal(t, "shared", uploaded.Visibility)
	assert.False(t, uploaded.Protected)
	require.NotEmpty(t, uploaded.ImageID)
	image, err := images.Get(ctx, imageClient, uploaded.ImageID).Extract()
	require.NoError(t, err)
	assert.Equal(t, images.ImageStatusActive, image.Status)
	assert.Equal(t, "volume upload", image.Name)
	assert.Equal(t, "bare", image.ContainerFormat)
	assert.Equal(t, "raw", image.DiskFormat)
	assert.Equal(t, images.ImageVisibilityShared, image.Visibility)
	assert.Equal(t, before.TenantID, image.Owner)
	assert.Equal(t, source.SizeBytes, image.SizeBytes)
	assert.Equal(t, source.Checksum, image.Checksum)
	assert.Equal(t, 0, image.MinDiskGigabytes)
	assert.Empty(t, image.Properties)
	assert.Equal(t, readData(fixtureImageID), readData(uploaded.ImageID))
	after, err := volumes.Get(ctx, storage, created.ID).Extract()
	require.NoError(t, err)
	assert.Equal(t, "available", after.Status)
	assert.False(t, after.UpdatedAt.Before(before.UpdatedAt))

	blank, err := volumes.UploadImage(ctx, storage, fixtureVolumeID, volumes.UploadImageOpts{
		ImageName: "blank upload", DiskFormat: "qcow2", ContainerFormat: "ovf",
	}).Extract()
	require.NoError(t, err)
	blankImage, err := images.Get(ctx, imageClient, blank.ImageID).Extract()
	require.NoError(t, err)

	assert.Equal(t, images.ImageStatusActive, blankImage.Status)
	assert.Equal(t, "ovf", blankImage.ContainerFormat)
	assert.Equal(t, "qcow2", blankImage.DiskFormat)
	assert.Equal(t, int64(0), blankImage.SizeBytes)
	assert.Equal(t, "d41d8cd98f00b204e9800998ecf8427e", blankImage.Checksum)

	_, err = volumeattach.Create(ctx, compute, fixtureServerID, volumeattach.CreateOpts{
		VolumeID: fixtureVolumeID,
	}).Extract()
	require.NoError(t, err)
	_, forcedErr := volumes.UploadImage(ctx, storage, fixtureVolumeID, volumes.UploadImageOpts{
		ImageName: "attached upload", Force: true,
	}).Extract()
	attached, err := volumes.Get(ctx, storage, fixtureVolumeID).Extract()
	require.NoError(t, err)

	assert.True(t, gophercloud.ResponseCodeIs(forcedErr, http.StatusBadRequest), "%v", forcedErr)
	assert.Equal(t, "in-use", attached.Status)

	require.NoError(t, images.Delete(ctx, imageClient, uploaded.ImageID).ExtractErr())
	require.NoError(t, images.Delete(ctx, imageClient, blank.ImageID).ExtractErr())
}

// https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/api/schemas/volume_actions.py#L162-L192
// https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/api/contrib/volume_actions.py#L199-L269
// https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/volume/api.py#L1483-L1552
// https://github.com/openstack/glance/blob/12.0.0/glance/common/config.py#L44-L56
func TestCinderVolumeUploadImageWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	projectID := volumeProjectID(t, provider)
	collection := volumePath + "/" + projectID + "/volumes"
	detail := collection + "/" + fixtureVolumeID
	action := detail + "/action"
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	listImages := func() []string {
		t.Helper()
		status, _, body := request(t, instance, http.MethodGet, imagesPath, "", auth)
		require.Equal(t, http.StatusOK, status, "%s", body)
		var response struct {
			Images []struct {
				ID string `json:"id"`
			} `json:"images"`
		}
		require.NoError(t, json.Unmarshal(body, &response))
		ids := make([]string, 0, len(response.Images))
		for _, image := range response.Images {
			ids = append(ids, image.ID)
		}
		return ids
	}
	readVolume := func() wireVolume {
		t.Helper()
		status, _, body := request(t, instance, http.MethodGet, detail, "", auth)
		require.Equal(t, http.StatusOK, status, "%s", body)
		var response struct {
			Volume wireVolume `json:"volume"`
		}
		require.NoError(t, json.Unmarshal(body, &response))
		return response.Volume
	}
	status, _, body := request(t, instance, http.MethodPost, collection, `{"volume":{"size":1}}`, auth)
	require.Equal(t, http.StatusAccepted, status, "%s", body)
	var creating struct {
		Volume wireVolume `json:"volume"`
	}
	require.NoError(t, json.Unmarshal(body, &creating))
	baselineImages := listImages()
	baselineVolume := readVolume()

	for _, tc := range []struct {
		name   string
		path   string
		body   string
		status int
	}{
		{"null action", action, `{"os-volume_upload_image":null}`, http.StatusBadRequest},
		{"missing image name", action, `{"os-volume_upload_image":{}}`, http.StatusBadRequest},
		{"empty image name", action, `{"os-volume_upload_image":{"image_name":""}}`, http.StatusBadRequest},
		{"numeric image name", action, `{"os-volume_upload_image":{"image_name":1}}`, http.StatusBadRequest},
		{"invalid force", action, `{"os-volume_upload_image":{"image_name":"i","force":"maybe"}}`, http.StatusBadRequest},
		{"unknown disk format", action, `{"os-volume_upload_image":{"image_name":"i","disk_format":"iso"}}`, http.StatusBadRequest},
		{"null disk format", action, `{"os-volume_upload_image":{"image_name":"i","disk_format":null}}`, http.StatusBadRequest},
		{"glance disk format", action, `{"os-volume_upload_image":{"image_name":"i","disk_format":"ploop"}}`, http.StatusBadRequest},
		{"glance container format", action, `{"os-volume_upload_image":{"image_name":"i","container_format":"compressed"}}`, http.StatusBadRequest},
		{"empty container format", action, `{"os-volume_upload_image":{"image_name":"i","container_format":""}}`, http.StatusBadRequest},
		{"visibility at 3.0", action, `{"os-volume_upload_image":{"image_name":"i","visibility":"private"}}`, http.StatusBadRequest},
		{"protected at 3.0", action, `{"os-volume_upload_image":{"image_name":"i","protected":false}}`, http.StatusBadRequest},
		{"creating volume", collection + "/" + creating.Volume.ID + "/action", `{"os-volume_upload_image":{"image_name":"i"}}`, http.StatusBadRequest},
		{"missing volume", collection + "/" + missingVolumeMarker + "/action", `{"os-volume_upload_image":{"image_name":"i"}}`, http.StatusNotFound},
		{"missing volume empty container", collection + "/" + missingVolumeMarker + "/action", `{"os-volume_upload_image":{"image_name":"i","container_format":""}}`, http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, headers, body := request(t, instance, http.MethodPost, tc.path, tc.body, auth)

			assert.Equal(t, tc.status, status, "%s", body)
			requireJSON(t, headers)
		})
	}
	status, _, body = request(t, instance, http.MethodPost, collection+"/"+creating.Volume.ID+"/action",
		`{"os-volume_upload_image":{"image_name":"i"}}`, auth)
	require.Equal(t, http.StatusBadRequest, status, "%s", body)
	assert.JSONEq(t, `{"badRequest":{"code":400,"message":"Invalid volume: Volume `+creating.Volume.ID+
		` status must be available"}}`, string(body))
	assert.Equal(t, baselineImages, listImages())
	assert.Equal(t, baselineVolume, readVolume())

	status, headers, body := request(t, instance, http.MethodPost, action,
		`{"os-volume_upload_image":{"image_name":"wire upload","force":"false","disk_format":"vhd"}}`, auth)

	require.Equal(t, http.StatusAccepted, status, "%s", body)
	requireJSON(t, headers)
	var accepted struct {
		UploadImage map[string]json.RawMessage `json:"os-volume_upload_image"`
	}
	require.NoError(t, json.Unmarshal(body, &accepted))
	var imageID string
	require.NoError(t, json.Unmarshal(accepted.UploadImage["image_id"], &imageID))
	assert.ElementsMatch(t, []string{
		"id", "updated_at", "status", "display_description", "size", "volume_type", "image_id",
		"container_format", "disk_format", "image_name", "protected", "visibility",
	}, slices.Collect(maps.Keys(accepted.UploadImage)))
	assert.JSONEq(t, `"`+fixtureVolumeID+`"`, string(accepted.UploadImage["id"]))
	assert.JSONEq(t, `"`+baselineVolume.UpdatedAt+`"`, string(accepted.UploadImage["updated_at"]))
	assert.JSONEq(t, `"uploading"`, string(accepted.UploadImage["status"]))
	assert.JSONEq(t, `null`, string(accepted.UploadImage["display_description"]))
	assert.JSONEq(t, `1`, string(accepted.UploadImage["size"]))
	assert.JSONEq(t, `{
		"id": "`+fixtureVolumeTypeID+`",
		"name": "standard",
		"description": "Standard SandStack volume type",
		"is_public": true,
		"extra_specs": {},
		"qos_specs_id": null,
		"deleted": false,
		"deleted_at": null
	}`, string(accepted.UploadImage["volume_type"]))
	assert.JSONEq(t, `"bare"`, string(accepted.UploadImage["container_format"]))
	assert.JSONEq(t, `"vhd"`, string(accepted.UploadImage["disk_format"]))
	assert.JSONEq(t, `"wire upload"`, string(accepted.UploadImage["image_name"]))
	assert.JSONEq(t, `false`, string(accepted.UploadImage["protected"]))
	assert.JSONEq(t, `"shared"`, string(accepted.UploadImage["visibility"]))
	assert.Contains(t, listImages(), imageID)
	assert.Equal(t, "available", readVolume().Status)

	status, _, body = request(t, instance, http.MethodPost, action,
		`{"os-volume_upload_image":{"image_name":"no container","container_format":null}}`, auth)
	require.Equal(t, http.StatusAccepted, status, "%s", body)
	var failed struct {
		UploadImage struct {
			ImageID         string  `json:"image_id"`
			ContainerFormat *string `json:"container_format"`
		} `json:"os-volume_upload_image"`
	}
	require.NoError(t, json.Unmarshal(body, &failed))

	assert.Nil(t, failed.UploadImage.ContainerFormat)
	assert.NotContains(t, listImages(), failed.UploadImage.ImageID)
	status, _, body = request(t, instance, http.MethodGet, imagesPath+"/"+failed.UploadImage.ImageID, "", auth)
	assert.Equal(t, http.StatusNotFound, status, "%s", body)
	assert.Equal(t, "available", readVolume().Status)

	status, _, body = request(t, instance, http.MethodPost, "/_sandstack/reset", "",
		map[string]string{"X-SandStack-Admin-Token": adminToken})
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	auth["X-Auth-Token"] = authenticate(t, instance).Token()

	assert.NotContains(t, listImages(), imageID)
}
