package server_test

import (
	"bytes"
	"io"
	"net/http"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/image/v2/imagedata"
	"github.com/gophercloud/gophercloud/v2/openstack/image/v2/images"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const unavailableImageDownloadControl = "/_sandstack/image/images/" + fixtureImageID + "/unavailable-download"

func TestGophercloudRetriesGlanceDownloadAfterOneTransientFailure(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	client, err := openstack.NewImageV2(provider, gophercloud.EndpointOpts{
		Region:       "RegionOne",
		Availability: gophercloud.AvailabilityPublic,
	})
	require.NoError(t, err)
	ctx := t.Context()
	readBytes := func() []byte {
		t.Helper()
		reader, err := imagedata.Download(ctx, client, fixtureImageID).Extract()
		require.NoError(t, err)
		require.NotNil(t, reader)
		data, readErr := io.ReadAll(reader)
		closeErr := reader.Close()
		require.NoError(t, readErr)
		require.NoError(t, closeErr)
		return data
	}
	baseline := readBytes()
	require.Len(t, baseline, fixtureImageDataSize)
	metadata, err := images.Get(ctx, client, fixtureImageID).Extract()
	require.NoError(t, err)

	admin := map[string]string{"X-SandStack-Admin-Token": adminToken}
	for range 2 {
		status, _, body := request(t, instance, http.MethodPut, unavailableImageDownloadControl, "", admin)
		require.Equal(t, http.StatusNoContent, status, "%s", body)
	}
	pending, err := images.Get(ctx, client, fixtureImageID).Extract()
	require.NoError(t, err)
	assert.Equal(t, metadata, pending)

	_, err = imagedata.Download(ctx, client, fixtureImageID).Extract()
	require.True(t, gophercloud.ResponseCodeIs(err, http.StatusServiceUnavailable), "%v", err)
	assert.Equal(t, baseline, readBytes())
	recovered, err := images.Get(ctx, client, fixtureImageID).Extract()
	require.NoError(t, err)
	assert.Equal(t, metadata, recovered)
}

func TestGlanceDownloadTransientFailureWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	file := imagesPath + "/" + fixtureImageID + "/file"
	admin := map[string]string{"X-SandStack-Admin-Token": adminToken}
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	validRange := map[string]string{"X-Auth-Token": provider.Token(), "Range": "bytes=0-3"}
	invalidRange := map[string]string{"X-Auth-Token": provider.Token(), "Range": "bytes=3-0"}
	const deactivate = "/_sandstack/image/images/" + fixtureImageID + "/deactivated"
	control := func(method, path string) {
		t.Helper()
		status, _, body := request(t, instance, method, path, "", admin)
		require.Equal(t, http.StatusNoContent, status, "%s", body)
	}

	control(http.MethodPut, unavailableImageDownloadControl)
	control(http.MethodPut, deactivate)
	status, _, body := request(t, instance, http.MethodGet, file, "", validRange)
	require.Equal(t, http.StatusForbidden, status, "%s", body)
	control(http.MethodDelete, deactivate)

	for _, tt := range []struct {
		name, path string
		headers    map[string]string
		status     int
	}{
		{"missing token", file, nil, http.StatusUnauthorized},
		{"missing image", imagesPath + "/00000000-0000-0000-0000-000000000000/file", validRange, http.StatusNotFound},
		{"unsupported query", file + "?prefer=filesystem", validRange, http.StatusBadRequest},
		{"malformed query", file + "?prefer=%zz", validRange, http.StatusBadRequest},
		{"invalid range", file, invalidRange, http.StatusRequestedRangeNotSatisfiable},
	} {
		t.Run(tt.name, func(t *testing.T) {
			status, _, body := request(t, instance, http.MethodGet, tt.path, "", tt.headers)
			assert.Equal(t, tt.status, status, "%s", body)
		})
	}

	// https://github.com/openstack/glance/blob/stable/2025.1/glance/api/v2/image_data.py#L491-L525
	status, headers, body := request(t, instance, http.MethodGet, file, "", validRange)
	require.Equal(t, http.StatusServiceUnavailable, status, "%s", body)
	assert.Empty(t, headers.Get("Content-MD5"))
	assert.Empty(t, headers.Get("Content-Range"))
	assert.NotEqual(t, "application/octet-stream", headers.Get("Content-Type"))
	assert.False(t, bytes.Contains(body, []byte{'Q', 'F', 'I', 0xfb}), "response includes image bytes")

	status, headers, body = request(t, instance, http.MethodGet, file, "", validRange)
	require.Equal(t, http.StatusPartialContent, status, "%s", body)
	assert.Equal(t, "bytes 0-3/1792", headers.Get("Content-Range"))
	assert.Equal(t, fixtureImageMD5, headers.Get("Content-MD5"))
	assert.Equal(t, []byte{'Q', 'F', 'I', 0xfb}, body)

	control(http.MethodPut, unavailableImageDownloadControl)
	control(http.MethodDelete, unavailableImageDownloadControl)
	control(http.MethodDelete, unavailableImageDownloadControl)
	status, _, body = request(t, instance, http.MethodGet, file, "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	assert.Len(t, body, fixtureImageDataSize)

	control(http.MethodPut, unavailableImageDownloadControl)
	control(http.MethodPost, "/_sandstack/reset")
	status, _, body = request(t, instance, http.MethodGet, file, "", auth)
	require.Equal(t, http.StatusUnauthorized, status, "%s", body)
	fresh := authenticate(t, instance)
	status, headers, body = request(t, instance, http.MethodGet, file, "",
		map[string]string{"X-Auth-Token": fresh.Token()})
	require.Equal(t, http.StatusOK, status, "%s", body)
	assert.Equal(t, fixtureImageMD5, headers.Get("Content-MD5"))
	assert.Len(t, body, fixtureImageDataSize)
}
