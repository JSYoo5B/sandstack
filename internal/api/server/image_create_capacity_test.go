package server_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/image/v2/imagedata"
	"github.com/gophercloud/gophercloud/v2/openstack/image/v2/images"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGophercloudPreservesGlanceImageCapacityRequirements(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	client, err := openstack.NewImageV2(provider, gophercloud.EndpointOpts{
		Region: "RegionOne", Availability: gophercloud.AvailabilityPublic,
	})
	require.NoError(t, err)
	ctx := t.Context()
	const name = "image with capacity requirements"
	created, err := images.Create(ctx, client, images.CreateOpts{
		Name: name, ContainerFormat: "bare", DiskFormat: "qcow2",
		MinDisk: 2, MinRAM: 2048,
	}).Extract()
	require.NoError(t, err)
	require.NotEmpty(t, created.ID)

	checkCapacity := func(image *images.Image, wantStatus images.ImageStatus) {
		t.Helper()
		assert.Equal(t, created.ID, image.ID)
		assert.Equal(t, wantStatus, image.Status)
		assert.Equal(t, 2, image.MinDiskGigabytes)
		assert.Equal(t, 2048, image.MinRAMMegabytes)
	}
	readAndList := func(wantStatus images.ImageStatus) {
		t.Helper()
		image, err := images.Get(ctx, client, created.ID).Extract()
		require.NoError(t, err)
		checkCapacity(image, wantStatus)
		pages, err := images.List(client, images.ListOpts{Name: name}).AllPages(ctx)
		require.NoError(t, err)
		listed, err := images.ExtractImages(pages)
		require.NoError(t, err)
		require.Len(t, listed, 1)
		checkCapacity(&listed[0], wantStatus)
	}

	checkCapacity(created, images.ImageStatusQueued)
	readAndList(images.ImageStatusQueued)

	err = imagedata.Upload(ctx, client, created.ID, strings.NewReader("opaque image bytes")).ExtractErr()
	require.NoError(t, err)
	readAndList(images.ImageStatusActive)
}

func TestGlanceImageCapacityCreateWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	status, _, before := request(t, instance, http.MethodGet, imagesPath, "", auth)
	require.Equal(t, http.StatusOK, status, "%s", before)

	// https://docs.openstack.org/api-ref/image/v2/#create-image
	for _, field := range []string{"min_disk", "min_ram"} {
		for _, invalid := range []string{"-1", "null", `"2"`, "1.5"} {
			payload := fmt.Sprintf(`{"name":"rejected capacity",%q:%s}`, field, invalid)
			status, _, body := request(t, instance, http.MethodPost, imagesPath, payload, auth)
			assert.Equal(t, http.StatusBadRequest, status, "%s: %s", payload, body)
		}
	}
	status, _, after := request(t, instance, http.MethodGet, imagesPath, "", auth)
	require.Equal(t, http.StatusOK, status, "%s", after)
	assert.JSONEq(t, string(before), string(after))

	for _, payload := range []string{
		`{"name":"default capacity"}`,
		`{"name":"explicit zero capacity","min_disk":0,"min_ram":0}`,
	} {
		status, headers, body := request(t, instance, http.MethodPost, imagesPath, payload, auth)
		require.Equal(t, http.StatusCreated, status, "%s", body)
		requireJSON(t, headers)
		var image map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(body, &image))
		assert.Equal(t, "0", string(image["min_disk"]))
		assert.Equal(t, "0", string(image["min_ram"]))
	}
}
