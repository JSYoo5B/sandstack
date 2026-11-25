package server_test

import (
	"crypto/md5"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/servers"
	"github.com/gophercloud/gophercloud/v2/openstack/image/v2/imagedata"
	"github.com/gophercloud/gophercloud/v2/openstack/image/v2/images"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	fixtureImageDataSize = 1792
	fixtureImageMD5      = "463d9a94ab58a1f59050bfba0835a320"
)

func TestImageDownloadWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	path := imagesPath + "/" + fixtureImageID + "/file"
	auth := map[string]string{"X-Auth-Token": provider.Token()}

	// https://docs.openstack.org/api-ref/image/v2/#download-binary-image-data
	status, headers, body := request(t, instance, http.MethodGet, path, "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	assert.Equal(t, "application/octet-stream", headers.Get("Content-Type"))
	assert.Equal(t, strconv.Itoa(fixtureImageDataSize), headers.Get("Content-Length"))
	assert.Equal(t, fixtureImageMD5, headers.Get("Content-MD5"))
	require.Len(t, body, fixtureImageDataSize)
	assert.Equal(t, []byte{'Q', 'F', 'I', 0xfb}, body[:4])
	assert.Equal(t, fixtureImageMD5, fmt.Sprintf("%x", md5.Sum(body)))

	const missingID = "00000000-0000-0000-0000-000000000000"
	status, _, body = request(t, instance, http.MethodGet, imagesPath+"/"+missingID+"/file", "", auth)
	assert.Equal(t, http.StatusNotFound, status, "%s", body)
	status, headers, body = request(t, instance, http.MethodGet, path, "", nil)
	assert.Equal(t, http.StatusUnauthorized, status, "%s", body)
	assert.Equal(t, "Keystone", headers.Get("WWW-Authenticate"))

	status, _, body = request(t, instance, http.MethodGet, path+"?prefer=filesystem", "", auth)
	assert.Equal(t, http.StatusBadRequest, status, "%s", body)
}

func TestGophercloudDownloadsImageReferencedByNova(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	endpoint := gophercloud.EndpointOpts{Region: "RegionOne", Availability: gophercloud.AvailabilityPublic}
	compute, err := openstack.NewComputeV2(provider, endpoint)
	require.NoError(t, err)
	server, err := servers.Get(t.Context(), compute, fixtureServerID).Extract()
	require.NoError(t, err)
	require.NotNil(t, server)
	imageID, ok := server.Image["id"].(string)
	require.True(t, ok)
	require.Equal(t, fixtureImageID, imageID)

	discover := func(provider *gophercloud.ProviderClient) *gophercloud.ServiceClient {
		t.Helper()
		client, err := openstack.NewImageV2(provider, endpoint)
		require.NoError(t, err)
		return client
	}
	imageClient := discover(provider)
	readImage := func(client *gophercloud.ServiceClient) *images.Image {
		t.Helper()
		image, err := images.Get(t.Context(), client, imageID).Extract()
		require.NoError(t, err)
		require.NotNil(t, image)
		return image
	}
	download := func(client *gophercloud.ServiceClient) []byte {
		t.Helper()
		result := imagedata.Download(t.Context(), client, imageID)
		reader, err := result.Extract()
		require.NoError(t, err)
		require.NotNil(t, reader)
		data, err := io.ReadAll(reader)
		closeErr := reader.Close()
		require.NoError(t, err)
		require.NoError(t, closeErr)
		return data
	}
	metadata := readImage(imageClient)
	baseline := download(imageClient)
	assert.Equal(t, images.ImageStatusActive, metadata.Status)
	assert.Equal(t, "qcow2", metadata.DiskFormat)
	assert.EqualValues(t, len(baseline), metadata.SizeBytes)
	assert.Equal(t, fmt.Sprintf("%x", md5.Sum(baseline)), metadata.Checksum)

	deactivate := "/_sandstack/image/images/" + imageID + "/deactivated"
	admin := map[string]string{"X-SandStack-Admin-Token": adminToken}
	control := func(method, path string) {
		t.Helper()
		status, _, body := request(t, instance, method, path, "", admin)
		require.Equal(t, http.StatusNoContent, status, "%s", body)
	}
	control(http.MethodPut, deactivate)
	down := readImage(imageClient)
	assert.Equal(t, images.ImageStatusDeactivated, down.Status)
	assert.Equal(t, metadata.SizeBytes, down.SizeBytes)
	assert.Equal(t, metadata.Checksum, down.Checksum)
	_, err = imagedata.Download(t.Context(), imageClient, imageID).Extract()
	require.Error(t, err)
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusForbidden), "%v", err)
	control(http.MethodDelete, deactivate)
	assert.Equal(t, images.ImageStatusActive, readImage(imageClient).Status)
	assert.Equal(t, baseline, download(imageClient))

	control(http.MethodPut, deactivate)
	control(http.MethodPost, "/_sandstack/reset")
	_, err = imagedata.Download(t.Context(), imageClient, imageID).Extract()
	require.Error(t, err)
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusUnauthorized), "%v", err)
	current := discover(authenticate(t, instance))
	assert.Equal(t, metadata, readImage(current))
	assert.Equal(t, baseline, download(current))
}
