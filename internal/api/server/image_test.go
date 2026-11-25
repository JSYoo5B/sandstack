package server_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/servers"
	"github.com/gophercloud/gophercloud/v2/openstack/identity/v3/tokens"
	"github.com/gophercloud/gophercloud/v2/openstack/image/v2/images"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	imagePath      = "/image"
	imagesPath     = imagePath + "/v2/images"
	fixtureImageID = "962a0750-d2e0-4400-88e0-a9ad35758eac"
)

func TestImageVersionDiscoveryWire(t *testing.T) {
	const publicOrigin = "https://public.glance.test"
	instance := startServer(t, publicOrigin)

	// https://docs.openstack.org/api-ref/image/versions/index.html
	status, headers, body := request(t, instance, http.MethodGet, imagePath+"/", "", nil)

	require.Equal(t, http.StatusMultipleChoices, status, "%s", body)
	requireJSON(t, headers)
	var discovery struct {
		Versions []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
			Links  []struct {
				Rel  string `json:"rel"`
				Href string `json:"href"`
			} `json:"links"`
		} `json:"versions"`
	}
	require.NoError(t, json.Unmarshal(body, &discovery))
	require.Len(t, discovery.Versions, 1)
	version := discovery.Versions[0]
	assert.Equal(t, "v2.0", version.ID)
	assert.Equal(t, "CURRENT", version.Status)
	require.Len(t, version.Links, 1)
	assert.Equal(t, "self", version.Links[0].Rel)
	assert.Equal(t, publicOrigin+imagePath+"/v2/", version.Links[0].Href)
}

func TestImageDetailsWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	issued, ok := provider.GetAuthResult().(tokens.CreateResult)
	require.True(t, ok)
	project, err := issued.ExtractProject()
	require.NoError(t, err)
	require.NotNil(t, project)
	require.NotEmpty(t, project.ID)
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	detailPath := imagesPath + "/" + fixtureImageID

	// https://docs.openstack.org/api-ref/image/v2/#show-image
	status, headers, body := request(t, instance, http.MethodGet, detailPath, "", auth)

	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	var image struct {
		ID              string          `json:"id"`
		Name            string          `json:"name"`
		Owner           string          `json:"owner"`
		Visibility      string          `json:"visibility"`
		Status          string          `json:"status"`
		Created         time.Time       `json:"created_at"`
		Updated         time.Time       `json:"updated_at"`
		DiskFormat      string          `json:"disk_format"`
		ContainerFormat string          `json:"container_format"`
		Self            string          `json:"self"`
		File            string          `json:"file"`
		Schema          string          `json:"schema"`
		Tags            []string        `json:"tags"`
		Protected       *bool           `json:"protected"`
		Checksum        string          `json:"checksum"`
		Size            *int            `json:"size"`
		Wrapper         json.RawMessage `json:"image"`
	}
	require.NoError(t, json.Unmarshal(body, &image))
	assert.Equal(t, fixtureImageID, image.ID)
	assert.Equal(t, "fixture-image", image.Name)
	assert.Equal(t, project.ID, image.Owner)
	assert.Equal(t, "private", image.Visibility)
	assert.Equal(t, "active", image.Status)
	assert.False(t, image.Created.IsZero())
	assert.False(t, image.Updated.Before(image.Created))
	assert.Equal(t, "qcow2", image.DiskFormat)
	assert.Equal(t, "bare", image.ContainerFormat)
	assert.Equal(t, detailPath, image.Self)
	assert.Equal(t, detailPath+"/file", image.File)
	assert.Equal(t, imagePath+"/v2/schemas/image", image.Schema)
	assert.Equal(t, []string{}, image.Tags)
	require.NotNil(t, image.Protected)
	assert.False(t, *image.Protected)
	assert.Equal(t, fixtureImageMD5, image.Checksum)
	require.NotNil(t, image.Size)
	assert.Equal(t, fixtureImageDataSize, *image.Size)
	assert.Empty(t, image.Wrapper)

	const missingID = "00000000-0000-0000-0000-000000000000"
	status, _, body = request(t, instance, http.MethodGet, imagesPath+"/"+missingID, "", auth)

	assert.Equal(t, http.StatusNotFound, status, "%s", body)

	status, headers, body = request(t, instance, http.MethodGet, detailPath, "", nil)

	assert.Equal(t, http.StatusUnauthorized, status, "%s", body)
	assert.Equal(t, "Keystone", headers.Get("WWW-Authenticate"))
}

func TestGophercloudFollowsNovaImageThroughDeactivationAndReset(t *testing.T) {
	const resetPath = "/_sandstack/reset"
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	endpoint := gophercloud.EndpointOpts{Region: "RegionOne", Availability: gophercloud.AvailabilityPublic}
	compute, err := openstack.NewComputeV2(provider, endpoint)
	require.NoError(t, err)
	server, err := servers.Get(t.Context(), compute, fixtureServerID).Extract()
	require.NoError(t, err)
	require.NotEmpty(t, server.TenantID)
	imageID, ok := server.Image["id"].(string)
	require.True(t, ok)
	require.NotEmpty(t, imageID)

	discover := func(provider *gophercloud.ProviderClient) *gophercloud.ServiceClient {
		t.Helper()
		client, err := openstack.NewImageV2(provider, endpoint)
		require.NoError(t, err)
		assert.Equal(t, instance.URL+imagePath+"/", client.Endpoint)
		assert.Equal(t, instance.URL+imagePath+"/v2/", client.ResourceBase)
		return client
	}
	readImage := func(client *gophercloud.ServiceClient) *images.Image {
		t.Helper()
		image, err := images.Get(t.Context(), client, imageID).Extract()
		require.NoError(t, err)
		return image
	}

	client := discover(provider)
	baseline := readImage(client)

	assert.Equal(t, fixtureImageID, baseline.ID)
	assert.Equal(t, imageID, baseline.ID)
	assert.Equal(t, "fixture-image", baseline.Name)
	assert.Equal(t, server.TenantID, baseline.Owner)
	assert.Equal(t, images.ImageVisibilityPrivate, baseline.Visibility)
	assert.Equal(t, images.ImageStatusActive, baseline.Status)
	assert.False(t, baseline.CreatedAt.IsZero())
	assert.False(t, baseline.UpdatedAt.IsZero())

	controlPath := "/_sandstack/image/images/" + imageID + "/deactivated"
	status, _, body := request(t, instance, http.MethodPut, controlPath, "", map[string]string{
		"X-SandStack-Admin-Token": provider.Token(),
	})

	require.Equal(t, http.StatusUnauthorized, status, "%s", body)
	assert.Equal(t, baseline, readImage(client))

	admin := map[string]string{"X-SandStack-Admin-Token": adminToken}
	status, _, body = request(t, instance, http.MethodPut, controlPath, "", admin)

	require.Equal(t, http.StatusNoContent, status, "%s", body)
	deactivated := readImage(client)
	assert.Equal(t, baseline.ID, deactivated.ID)
	assert.Equal(t, baseline.Owner, deactivated.Owner)
	assert.Equal(t, baseline.CreatedAt, deactivated.CreatedAt)
	assert.Equal(t, images.ImageStatusDeactivated, deactivated.Status)
	sameServer, err := servers.Get(t.Context(), compute, fixtureServerID).Extract()
	require.NoError(t, err)
	assert.Equal(t, server, sameServer)

	status, _, body = request(t, instance, http.MethodDelete, controlPath, "", admin)

	require.Equal(t, http.StatusNoContent, status, "%s", body)
	reactivated := readImage(client)
	assert.Equal(t, baseline.ID, reactivated.ID)
	assert.Equal(t, images.ImageStatusActive, reactivated.Status)

	status, _, body = request(t, instance, http.MethodPut, controlPath, "", admin)
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	require.Equal(t, images.ImageStatusDeactivated, readImage(client).Status)
	status, _, body = request(t, instance, http.MethodPost, resetPath, "", admin)

	require.Equal(t, http.StatusNoContent, status, "%s", body)
	_, err = images.Get(t.Context(), client, imageID).Extract()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusUnauthorized), "%v", err)
	assert.Equal(t, baseline, readImage(discover(authenticate(t, instance))))
}
