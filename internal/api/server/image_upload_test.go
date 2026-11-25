package server_test

import (
	"bytes"
	"crypto/md5"
	"fmt"
	"io"
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

func TestGophercloudUploadsAndDownloadsGlanceImage(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	client, err := openstack.NewImageV2(provider, gophercloud.EndpointOpts{
		Region: "RegionOne", Availability: gophercloud.AvailabilityPublic,
	})
	require.NoError(t, err)
	ctx := t.Context()
	created, err := images.Create(ctx, client, images.CreateOpts{
		Name: "uploaded SDK image", DiskFormat: "qcow2", ContainerFormat: "bare",
	}).Extract()
	require.NoError(t, err)
	require.Equal(t, images.ImageStatusQueued, created.Status)
	payload := []byte{0x51, 0x46, 0x49, 0xfb, 0, 0xff, 0x10, 0x80, 0x7f}
	checksum := fmt.Sprintf("%x", md5.Sum(payload))

	stream := io.LimitReader(bytes.NewReader(payload), int64(len(payload)))
	require.NoError(t, imagedata.Upload(ctx, client, created.ID, stream).ExtractErr())

	active, err := images.Get(ctx, client, created.ID).Extract()
	require.NoError(t, err)
	assert.Equal(t, created.ID, active.ID)
	assert.Equal(t, created.Owner, active.Owner)
	assert.Equal(t, created.Name, active.Name)
	assert.Equal(t, created.CreatedAt, active.CreatedAt)
	assert.Equal(t, images.ImageStatusActive, active.Status)
	assert.Equal(t, checksum, active.Checksum)
	assert.EqualValues(t, len(payload), active.SizeBytes)
	pages, err := images.List(client, images.ListOpts{Name: created.Name, Status: images.ImageStatusActive}).AllPages(ctx)
	require.NoError(t, err)
	listed, err := images.ExtractImages(pages)
	require.NoError(t, err)
	assert.Equal(t, []images.Image{*active}, listed)

	reader, err := imagedata.Download(ctx, client, created.ID).Extract()
	require.NoError(t, err)
	downloaded, err := io.ReadAll(reader)
	closeErr := reader.Close()
	require.NoError(t, err)
	require.NoError(t, closeErr)
	assert.Equal(t, payload, downloaded)

	path := imagesPath + "/" + created.ID + "/file"
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	status, headers, body := request(t, instance, http.MethodGet, path, "", map[string]string{
		"X-Auth-Token": provider.Token(), "Range": "bytes=3-6",
	})
	require.Equal(t, http.StatusPartialContent, status, "%s", body)
	assert.Equal(t, "bytes 3-6/9", headers.Get("Content-Range"))
	assert.Equal(t, "4", headers.Get("Content-Length"))
	assert.Equal(t, checksum, headers.Get("Content-MD5"))
	assert.Equal(t, payload[3:7], body)

	require.NoError(t, images.Delete(ctx, client, created.ID).ExtractErr())

	status, _, body = request(t, instance, http.MethodGet, path, "", auth)
	assert.Equal(t, http.StatusNotFound, status, "%s", body)
}

func TestGlanceImageUploadWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	endpoint := gophercloud.EndpointOpts{Region: "RegionOne", Availability: gophercloud.AvailabilityPublic}
	client, err := openstack.NewImageV2(provider, endpoint)
	require.NoError(t, err)
	prepare := func(opts images.CreateOpts) *images.Image {
		t.Helper()
		result, err := images.Create(t.Context(), client, opts).Extract()
		require.NoError(t, err)
		require.Equal(t, images.ImageStatusQueued, result.Status)
		return result
	}
	prepared := prepare(images.CreateOpts{Name: "ready for upload", ContainerFormat: "bare", DiskFormat: "qcow2"})
	noFormats := prepare(images.CreateOpts{Name: "missing disk and container formats"})
	path := imagesPath + "/" + prepared.ID + "/file"
	noFormatsPath := imagesPath + "/" + noFormats.ID + "/file"
	const binary = "application/octet-stream"
	send := func(path, mediaType, token, data string) (int, []byte) {
		t.Helper()
		req, err := http.NewRequestWithContext(t.Context(), http.MethodPut, instance.URL+path, strings.NewReader(data))
		require.NoError(t, err)
		if mediaType != "" {
			req.Header.Set("Content-Type", mediaType)
		}
		if token != "" {
			req.Header.Set("X-Auth-Token", token)
		}
		response, err := instance.Client().Do(req)
		require.NoError(t, err)
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		require.NoError(t, err)
		return response.StatusCode, body
	}

	// https://docs.openstack.org/api-ref/image/v2/#upload-binary-image-data
	const unknown = imagesPath + "/00000000-0000-0000-0000-000000000000/file"
	for _, test := range []struct {
		name      string
		path      string
		mediaType string
		token     string
		want      int
	}{
		{"missing media type", path, "", provider.Token(), http.StatusUnsupportedMediaType},
		{"wrong media type", path, "text/plain", provider.Token(), http.StatusUnsupportedMediaType},
		{"missing image formats", noFormatsPath, binary, provider.Token(), http.StatusBadRequest},
		{"missing image", unknown, binary, provider.Token(), http.StatusNotFound},
		{"missing authentication", path, binary, "", http.StatusUnauthorized},
	} {
		t.Run(test.name, func(t *testing.T) {
			status, body := send(test.path, test.mediaType, test.token, "rejected data")
			assert.Equal(t, test.want, status, "%s", body)
		})
	}
	status, body := send(path, binary, provider.Token(), strings.Repeat("x", (16<<20)+1))
	assert.Equal(t, http.StatusRequestEntityTooLarge, status, "%s", body)
	for _, queued := range []*images.Image{prepared, noFormats} {
		detail, err := images.Get(t.Context(), client, queued.ID).Extract()
		require.NoError(t, err)
		assert.Equal(t, queued, detail)
	}

	status, body = send(path, binary, provider.Token(), "")
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	assert.Empty(t, body)
	status, body = send(path, binary, provider.Token(), "second upload")
	assert.Equal(t, http.StatusConflict, status, "%s", body)
	active, err := images.Get(t.Context(), client, prepared.ID).Extract()
	require.NoError(t, err)
	assert.Equal(t, images.ImageStatusActive, active.Status)
	assert.Zero(t, active.SizeBytes)
	const emptyChecksum = "d41d8cd98f00b204e9800998ecf8427e"
	assert.Equal(t, emptyChecksum, active.Checksum)
	status, headers, body := request(t, instance, http.MethodGet, path, "", map[string]string{
		"X-Auth-Token": provider.Token(),
	})
	require.Equal(t, http.StatusOK, status, "%s", body)
	assert.Equal(t, "0", headers.Get("Content-Length"))
	assert.Equal(t, emptyChecksum, headers.Get("Content-MD5"))
	assert.Empty(t, body)

	status, _, body = request(t, instance, http.MethodPost, "/_sandstack/reset", "", map[string]string{
		"X-SandStack-Admin-Token": adminToken,
	})
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	fresh := authenticate(t, instance)
	status, _, body = request(t, instance, http.MethodGet, path, "", map[string]string{
		"X-Auth-Token": fresh.Token(),
	})
	assert.Equal(t, http.StatusNotFound, status, "%s", body)
}
