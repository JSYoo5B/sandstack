package server_test

import (
	"bytes"
	"encoding/json"
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

func TestGophercloudProtectsGlanceImageFromDeletion(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	client, err := openstack.NewImageV2(provider, gophercloud.EndpointOpts{
		Region:       "RegionOne",
		Availability: gophercloud.AvailabilityPublic,
	})
	require.NoError(t, err)
	ctx := t.Context()
	protected := true
	created, err := images.Create(ctx, client, images.CreateOpts{
		Name: "protected SDK image", ContainerFormat: "bare", DiskFormat: "qcow2", Protected: &protected,
	}).Extract()
	require.NoError(t, err)
	require.True(t, created.Protected)
	payload := []byte{0x51, 0x46, 0x49, 0xfb, 0x00, 0xff}
	require.NoError(t, imagedata.Upload(ctx, client, created.ID, bytes.NewReader(payload)).ExtractErr())
	active, err := images.Get(ctx, client, created.ID).Extract()
	require.NoError(t, err)
	require.Equal(t, images.ImageStatusActive, active.Status)
	require.True(t, active.Protected)

	err = images.Delete(ctx, client, active.ID).ExtractErr()
	require.True(t, gophercloud.ResponseCodeIs(err, http.StatusForbidden), "%v", err)

	preserved, err := images.Get(ctx, client, active.ID).Extract()
	require.NoError(t, err)
	assert.Equal(t, active, preserved)
	pages, err := images.List(client, images.ListOpts{ID: active.ID}).AllPages(ctx)
	require.NoError(t, err)
	listed, err := images.ExtractImages(pages)
	require.NoError(t, err)
	assert.Equal(t, []images.Image{*active}, listed)
	reader, err := imagedata.Download(ctx, client, active.ID).Extract()
	require.NoError(t, err)
	body, readErr := io.ReadAll(reader)
	closeErr := reader.Close()
	require.NoError(t, readErr)
	require.NoError(t, closeErr)
	assert.Equal(t, payload, body)

	unprotected, err := images.Update(ctx, client, active.ID, images.UpdateOpts{
		images.ReplaceImageProtected{NewProtected: false},
	}).Extract()
	require.NoError(t, err)
	expected := *active
	expected.Protected, expected.UpdatedAt = false, unprotected.UpdatedAt
	assert.Equal(t, expected, *unprotected)
	require.NoError(t, images.Delete(ctx, client, active.ID).ExtractErr())
	_, err = images.Get(ctx, client, active.ID).Extract()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusNotFound), "%v", err)
}

func TestGlanceImageProtectionWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	patchHeaders := map[string]string{
		"X-Auth-Token": provider.Token(),
		"Content-Type": "application/openstack-images-v2.1-json-patch",
	}
	status, headers, body := request(t, instance, http.MethodPost, imagesPath,
		`{"name":"wire-protection","protected":false}`, auth)
	require.Equal(t, http.StatusCreated, status, "%s", body)
	requireJSON(t, headers)
	var created struct {
		ID        string          `json:"id"`
		Protected json.RawMessage `json:"protected"`
	}
	require.NoError(t, json.Unmarshal(body, &created))
	require.NotEmpty(t, created.ID)
	assert.Equal(t, "false", string(created.Protected))
	path := imagesPath + "/" + created.ID

	patch := func(t *testing.T, value string) (int, http.Header, []byte) {
		t.Helper()
		payload := `[{"op":"replace","path":"/protected","value":` + value + `}]`
		return request(t, instance, http.MethodPatch, path, payload, patchHeaders)
	}
	status, headers, body = patch(t, "true")
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	protected := string(body)
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(body, &fields))
	assert.Equal(t, "true", string(fields["protected"]))

	for _, invalid := range []string{`null`, `"true"`, `1`} {
		t.Run(invalid, func(t *testing.T) {
			body := `{"name":"invalid-protection","protected":` + invalid + `}`
			status, _, data := request(t, instance, http.MethodPost, imagesPath, body, auth)
			assert.Equal(t, http.StatusBadRequest, status, "%s", data)

			status, _, data = patch(t, invalid)
			assert.Equal(t, http.StatusBadRequest, status, "%s", data)
		})
	}

	// https://docs.openstack.org/api-ref/image/v2/#delete-image
	status, headers, body = request(t, instance, http.MethodDelete, path, "", auth)
	require.Equal(t, http.StatusForbidden, status, "%s", body)
	assert.Contains(t, headers.Get("Content-Type"), "text/plain")
	assert.NotEmpty(t, body)
	status, _, body = request(t, instance, http.MethodGet, path, "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	assert.JSONEq(t, protected, string(body))

	status, headers, body = patch(t, "false")
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	fields = nil
	require.NoError(t, json.Unmarshal(body, &fields))
	assert.Equal(t, "false", string(fields["protected"]))
	status, _, body = request(t, instance, http.MethodDelete, path, "", auth)
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	assert.Empty(t, body)
}
