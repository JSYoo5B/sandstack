package server_test

import (
	"encoding/json"
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

func TestGophercloudRenamesImageWithoutChangingDataOrStatus(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	client, err := openstack.NewImageV2(provider, gophercloud.EndpointOpts{
		Region: "RegionOne", Availability: gophercloud.AvailabilityPublic,
	})
	require.NoError(t, err)
	ctx := t.Context()
	read := func() *images.Image {
		t.Helper()
		image, err := images.Get(ctx, client, fixtureImageID).Extract()
		require.NoError(t, err)
		return image
	}
	list := func(name string, status images.ImageStatus) []images.Image {
		t.Helper()
		pages, err := images.List(client, images.ListOpts{Name: name, Status: status}).AllPages(ctx)
		require.NoError(t, err)
		items, err := images.ExtractImages(pages)
		require.NoError(t, err)
		return items
	}
	download := func() []byte {
		t.Helper()
		reader, err := imagedata.Download(ctx, client, fixtureImageID).Extract()
		require.NoError(t, err)
		data, readErr := io.ReadAll(reader)
		closeErr := reader.Close()
		require.NoError(t, readErr)
		require.NoError(t, closeErr)
		return data
	}
	baseline := read()
	require.Equal(t, images.ImageStatusActive, baseline.Status)
	require.Equal(t, fixtureImageMD5, baseline.Checksum)
	file := download()
	require.Len(t, file, fixtureImageDataSize)
	oldName := baseline.Name
	const newName = "renamed image + fixture"
	rename := func(name string) *images.Image {
		t.Helper()
		revised, err := images.Update(ctx, client, fixtureImageID, images.UpdateOpts{
			images.ReplaceImageName{NewName: name},
		}).Extract()
		require.NoError(t, err)
		return revised
	}
	check := func(previous, revised *images.Image, name string) {
		t.Helper()
		want := *previous
		want.Name = name
		assert.False(t, revised.UpdatedAt.Before(previous.UpdatedAt))
		want.UpdatedAt = revised.UpdatedAt
		assert.Equal(t, &want, revised)
		assert.Equal(t, revised, read())
		assert.Equal(t, []images.Image{*revised}, list(name, revised.Status))
	}

	revised := rename(newName)

	check(baseline, revised, newName)
	assert.Empty(t, list(oldName, images.ImageStatusActive))
	assert.Equal(t, file, download())

	const control = "/_sandstack/image/images/" + fixtureImageID + "/deactivated"
	admin := map[string]string{"X-SandStack-Admin-Token": adminToken}
	status, _, body := request(t, instance, http.MethodPut, control, "", admin)
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	deactivated := read()
	require.Equal(t, images.ImageStatusDeactivated, deactivated.Status)
	const inactiveName = "renamed while deactivated"
	check(deactivated, rename(inactiveName), inactiveName)
	assert.Empty(t, list(newName, images.ImageStatusDeactivated))
	_, err = imagedata.Download(ctx, client, fixtureImageID).Extract()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusForbidden), "deactivated download: %v", err)
	status, _, body = request(t, instance, http.MethodDelete, control, "", admin)
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	assert.Equal(t, []images.Image{*read()}, list(inactiveName, images.ImageStatusActive))
	assert.Equal(t, file, download())
}

func TestImageNamePatchWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	const contentType = "application/openstack-images-v2.1-json-patch"
	auth := map[string]string{"X-Auth-Token": provider.Token(), "Content-Type": contentType}
	path := imagesPath + "/" + fixtureImageID
	status, headers, body := request(t, instance, http.MethodGet, path, "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	read := func(body []byte) map[string]json.RawMessage {
		t.Helper()
		var image map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(body, &image))
		require.Equal(t, `"`+fixtureImageID+`"`, string(image["id"]))
		require.NotContains(t, image, "image")
		return image
	}
	initial := read(body)
	const emptyName = `[{"op":"replace","path":"/name","value":""}]`
	status, headers, body = request(t, instance, http.MethodPatch, path, emptyName, map[string]string{
		"Content-Type": contentType,
	})
	require.Equal(t, http.StatusUnauthorized, status, "%s", body)
	assert.Equal(t, "Keystone", headers.Get("WWW-Authenticate"))
	for _, mediaType := range []string{
		"application/json", "application/json-patch+json", "application/openstack-images-v2.0-json-patch",
	} {
		status, headers, body = request(t, instance, http.MethodPatch, path, emptyName, map[string]string{
			"X-Auth-Token": provider.Token(), "Content-Type": mediaType,
		})
		require.Equal(t, http.StatusUnsupportedMediaType, status, "%s: %s", mediaType, body)
		assert.Equal(t, contentType, headers.Get("Accept-Patch"))
	}

	// https://docs.openstack.org/api-ref/image/v2/#update-an-image
	atLimit := strings.Repeat("한", 255)
	patch := func(name string) string {
		return fmt.Sprintf(`[{"op":"replace","path":"/name","value":%q}]`, name)
	}
	status, headers, body = request(t, instance, http.MethodPatch, path, patch(atLimit), auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	atLimitImage := read(body)
	name, err := json.Marshal(atLimit)
	require.NoError(t, err)
	assert.JSONEq(t, string(name), string(atLimitImage["name"]))
	initial["name"] = atLimitImage["name"]
	initial["updated_at"] = atLimitImage["updated_at"]
	assert.Equal(t, initial, atLimitImage)
	expected := string(body)
	status, headers, body = request(t, instance, http.MethodPatch, path, patch(atLimit), auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	assert.JSONEq(t, expected, string(body))

	for _, bad := range []string{
		`[]`,
		`{"op":"replace","path":"/name","value":"invalid"}`,
		`[{"op":"replace","path":"/name","value":}]`,
		`[{"op":"replace","path":"/name","value":null}]`,
		`[{"op":"replace","path":"/name","value":35}]`,
		`[{"op":"add","path":"/name","value":"invalid"}]`,
		`[{"op":"replace","path":"/status","value":"deactivated"}]`,
		`[{"op":"replace","path":"/name","value":"invalid","extra":true}]`,
		`[{"op":"replace","path":"/name","value":"one","value":"two"}]`,
		`[{"op":"replace","path":"/name","value":"one"},{"op":"replace","path":"/name","value":"two"}]`,
		patch(atLimit + "한"),
	} {
		status, _, body := request(t, instance, http.MethodPatch, path, bad, auth)
		require.Equal(t, http.StatusBadRequest, status, "%q: %s", bad, body)
	}
	status, headers, body = request(t, instance, http.MethodGet, path, "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	assert.JSONEq(t, expected, string(body))

	status, headers, body = request(t, instance, http.MethodPatch, path, emptyName, auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	assert.JSONEq(t, `""`, string(read(body)["name"]))
	const missing = "00000000-0000-0000-0000-000000000000"
	status, _, body = request(t, instance, http.MethodPatch, imagesPath+"/"+missing, emptyName, auth)
	require.Equal(t, http.StatusNotFound, status, "%s", body)
}
