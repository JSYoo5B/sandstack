package server_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/image/v2/images"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGophercloudCreatesAndListsQueuedGlanceImage(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	endpoint := gophercloud.EndpointOpts{Region: "RegionOne", Availability: gophercloud.AvailabilityPublic}
	client, err := openstack.NewImageV2(provider, endpoint)
	require.NoError(t, err)
	ctx := t.Context()
	baseline, err := images.Get(ctx, client, fixtureImageID).Extract()
	require.NoError(t, err)
	visibility := images.ImageVisibilityPrivate
	const name = "queued SDK image"

	created, err := images.Create(ctx, client, images.CreateOpts{
		Name: name, Visibility: &visibility, ContainerFormat: "bare", DiskFormat: "qcow2",
	}).Extract()

	require.NoError(t, err)
	require.NotEmpty(t, created.ID)
	assert.NotEqual(t, fixtureImageID, created.ID)
	assert.Equal(t, name, created.Name)
	assert.Equal(t, images.ImageStatusQueued, created.Status)
	assert.Equal(t, visibility, created.Visibility)
	assert.Equal(t, baseline.Owner, created.Owner)
	assert.Equal(t, "bare", created.ContainerFormat)
	assert.Equal(t, "qcow2", created.DiskFormat)
	assert.Empty(t, created.Checksum)
	assert.Zero(t, created.SizeBytes)
	assert.False(t, created.CreatedAt.IsZero())
	assert.False(t, created.UpdatedAt.Before(created.CreatedAt))
	detail, err := images.Get(ctx, client, created.ID).Extract()
	require.NoError(t, err)
	assert.Equal(t, created, detail)

	list := func(client *gophercloud.ServiceClient, opts images.ListOpts) []images.Image {
		t.Helper()
		pages, err := images.List(client, opts).AllPages(ctx)
		require.NoError(t, err)
		result, err := images.ExtractImages(pages)
		require.NoError(t, err)
		return result
	}
	assert.ElementsMatch(t, []images.Image{*baseline, *created}, list(client, images.ListOpts{Limit: 1}))
	assert.Equal(t, []images.Image{*created}, list(client, images.ListOpts{
		Status: images.ImageStatusQueued, Limit: 1,
	}))

	status, _, body := request(t, instance, http.MethodPost, "/_sandstack/reset", "", map[string]string{
		"X-SandStack-Admin-Token": adminToken,
	})
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	_, err = images.Get(ctx, client, created.ID).Extract()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusUnauthorized), "reset invalidates old token: %v", err)
	client, err = openstack.NewImageV2(authenticate(t, instance), endpoint)
	require.NoError(t, err)

	_, err = images.Get(ctx, client, created.ID).Extract()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusNotFound), "reset removes queued image: %v", err)
	assert.Equal(t, []images.Image{*baseline}, list(client, images.ListOpts{Limit: 1}))
}

func TestGlanceQueuedImageCreateWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	status, _, baseline := request(t, instance, http.MethodGet, imagesPath, "", auth)
	require.Equal(t, http.StatusOK, status, "%s", baseline)

	// https://docs.openstack.org/api-ref/image/v2/#create-image
	// https://github.com/openstack/glance/blob/master/glance/api/v2/images.py#L1674-L1692
	for _, item := range []struct {
		data   string
		status int
	}{
		{`{"name":"public is restricted","visibility":"public"}`, http.StatusForbidden},
		{`{"name":""}`, http.StatusBadRequest},
		{`{"name":9}`, http.StatusBadRequest},
		{`{"name":"readonly","status":"active"}`, http.StatusBadRequest},
		{`{"name":"wrong format","disk_format":"not-a-format"}`, http.StatusBadRequest},
		{`{"name":"invalid shape","tags":[false]}`, http.StatusBadRequest},
	} {
		status, _, body := request(t, instance, http.MethodPost, imagesPath, item.data, auth)
		assert.Equal(t, item.status, status, "%s: %s", item.data, body)
	}
	status, _, body := request(t, instance, http.MethodGet, imagesPath, "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	assert.JSONEq(t, string(baseline), string(body), "rejected requests leave the image collection intact")

	status, headers, body := request(t, instance, http.MethodPost, imagesPath, `{"name":"queued wire image"}`, auth)
	require.Equal(t, http.StatusCreated, status, "%s", body)
	requireJSON(t, headers)
	var created map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(body, &created))
	assert.NotContains(t, created, "image")
	var id string
	require.NoError(t, json.Unmarshal(created["id"], &id))
	require.NotEmpty(t, id)
	assert.NotEqual(t, fixtureImageID, id)
	assert.JSONEq(t, `"queued wire image"`, string(created["name"]))
	assert.JSONEq(t, `"queued"`, string(created["status"]))
	assert.JSONEq(t, `"shared"`, string(created["visibility"]))
	expectedOwner, err := json.Marshal(volumeProjectID(t, provider))
	require.NoError(t, err)
	assert.JSONEq(t, string(expectedOwner), string(created["owner"]))
	assert.JSONEq(t, `[]`, string(created["tags"]))
	for _, field := range []string{"size", "virtual_size", "checksum", "disk_format", "container_format"} {
		assert.JSONEq(t, `null`, string(created[field]), field)
	}
	var createdAt, updatedAt time.Time
	require.NoError(t, json.Unmarshal(created["created_at"], &createdAt))
	require.NoError(t, json.Unmarshal(created["updated_at"], &updatedAt))
	assert.False(t, createdAt.IsZero())
	assert.False(t, updatedAt.Before(createdAt))

	origin, err := url.Parse(instance.URL)
	require.NoError(t, err)
	require.NotEmpty(t, headers.Get("Location"))
	location, err := origin.Parse(headers.Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, location.String(), headers.Get("Location"))
	assert.Equal(t, instance.URL+imagesPath+"/"+id, location.String())
	status, headers, detail := request(t, instance, http.MethodGet, location.RequestURI(), "", auth)
	require.Equal(t, http.StatusOK, status, "%s", detail)
	requireJSON(t, headers)
	assert.JSONEq(t, string(body), string(detail))

	status, _, body = request(t, instance, http.MethodGet, imagesPath+"/"+id+"/file", "", auth)
	assert.Equal(t, http.StatusNoContent, status, "%s", body)
	assert.Empty(t, body)
}
