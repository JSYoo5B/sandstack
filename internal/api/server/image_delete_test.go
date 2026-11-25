package server_test

import (
	"net/http"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/servers"
	"github.com/gophercloud/gophercloud/v2/openstack/image/v2/images"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGophercloudDeletesQueuedGlanceImage(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	client, err := openstack.NewImageV2(provider, gophercloud.EndpointOpts{
		Region:       "RegionOne",
		Availability: gophercloud.AvailabilityPublic,
	})
	require.NoError(t, err)
	ctx := t.Context()
	visibility := images.ImageVisibilityPrivate
	created, err := images.Create(ctx, client, images.CreateOpts{
		Name: "queued-to-delete", Visibility: &visibility, ContainerFormat: "bare", DiskFormat: "qcow2",
	}).Extract()
	require.NoError(t, err)
	require.NotEmpty(t, created.ID)
	require.Equal(t, images.ImageStatusQueued, created.Status)
	seed, err := images.Get(ctx, client, fixtureImageID).Extract()
	require.NoError(t, err)

	list := func() []images.Image {
		t.Helper()
		pages, err := images.List(client, images.ListOpts{Limit: 1}).AllPages(ctx)
		require.NoError(t, err)
		rows, err := images.ExtractImages(pages)
		require.NoError(t, err)
		return rows
	}
	assert.ElementsMatch(t, []images.Image{*seed, *created}, list())

	require.NoError(t, images.Delete(ctx, client, created.ID).ExtractErr())
	_, err = images.Get(ctx, client, created.ID).Extract()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusNotFound), "%v", err)
	err = images.Delete(ctx, client, created.ID).ExtractErr()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusNotFound), "%v", err)
	assert.Equal(t, []images.Image{*seed}, list())

	_, err = images.List(client, images.ListOpts{Limit: 1, Marker: created.ID}).AllPages(ctx)
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusBadRequest), "%v", err)
}

func TestGlanceSeedImageDeletionWireAndReset(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	admin := map[string]string{"X-SandStack-Admin-Token": adminToken}
	path := imagesPath + "/" + fixtureImageID
	file := path + "/file"
	compute, err := openstack.NewComputeV2(provider, gophercloud.EndpointOpts{
		Region:       "RegionOne",
		Availability: gophercloud.AvailabilityPublic,
	})
	require.NoError(t, err)
	serverBefore, err := servers.Get(t.Context(), compute, fixtureServerID).Extract()
	require.NoError(t, err)
	assert.Equal(t, fixtureImageID, serverBefore.Image["id"])
	status, _, baseline := request(t, instance, http.MethodGet, file, "", auth)
	require.Equal(t, http.StatusOK, status, "%s", baseline)
	require.Len(t, baseline, fixtureImageDataSize)

	status, _, body := request(t, instance, http.MethodDelete, path, "", nil)
	require.Equal(t, http.StatusUnauthorized, status, "%s", body)
	status, _, body = request(t, instance, http.MethodDelete,
		imagesPath+"/00000000-0000-0000-0000-000000000000", "", auth)
	require.Equal(t, http.StatusNotFound, status, "%s", body)

	status, _, body = request(t, instance, http.MethodPut, unavailableImageDownloadControl, "", admin)
	require.Equal(t, http.StatusNoContent, status, "%s", body)

	// https://docs.openstack.org/api-ref/image/v2/#delete-image
	status, _, body = request(t, instance, http.MethodDelete, path, "", auth)
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	assert.Empty(t, body)

	for _, missing := range []string{path, file} {
		status, _, body = request(t, instance, http.MethodGet, missing, "", auth)
		assert.Equal(t, http.StatusNotFound, status, "%s", body)
	}
	for _, method := range []string{http.MethodPut, http.MethodDelete} {
		status, _, body = request(t, instance, method, unavailableImageDownloadControl, "", admin)
		assert.Equal(t, http.StatusNotFound, status, "%s", body)
	}
	serverAfter, err := servers.Get(t.Context(), compute, fixtureServerID).Extract()
	require.NoError(t, err)
	assert.Equal(t, serverBefore.Image, serverAfter.Image)

	status, _, body = request(t, instance, http.MethodPost, "/_sandstack/reset", "", admin)
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	status, _, body = request(t, instance, http.MethodGet, file, "", auth)
	require.Equal(t, http.StatusUnauthorized, status, "%s", body)
	fresh := authenticate(t, instance)
	status, headers, body := request(t, instance, http.MethodGet, file, "",
		map[string]string{"X-Auth-Token": fresh.Token()})
	require.Equal(t, http.StatusOK, status, "%s", body)
	assert.Equal(t, fixtureImageMD5, headers.Get("Content-MD5"))
	assert.Equal(t, baseline, body)
}
