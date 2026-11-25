package server_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/volumes"
	"github.com/gophercloud/gophercloud/v2/openstack/identity/v3/tokens"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	volumePath      = "/volume/v3"
	fixtureVolumeID = "7b37635e-be9b-4bfb-bc2e-d163b75921e6"
)

func TestVolumeWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	projectID := volumeProjectID(t, provider)
	collection := volumePath + "/" + projectID + "/volumes"
	auth := map[string]string{"X-Auth-Token": provider.Token()}

	// https://docs.openstack.org/api-ref/block-storage/v3/#list-accessible-volumes-with-details
	status, headers, body := request(t, instance, http.MethodGet, collection+"/detail", "", auth)

	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	assert.Equal(t, "volume 3.0", headers.Get("OpenStack-API-Version"))
	var listing struct {
		Volumes []wireVolume `json:"volumes"`
	}
	require.NoError(t, json.Unmarshal(body, &listing))
	require.Len(t, listing.Volumes, 1)
	baseline := listing.Volumes[0]
	assert.Equal(t, fixtureVolumeID, baseline.ID)
	assert.Equal(t, "fixture-volume", baseline.Name)
	assert.Equal(t, 1, baseline.Size)
	assert.Equal(t, "available", baseline.Status)
	assert.Equal(t, projectID, baseline.ProjectID)
	assert.Equal(t, "nova", baseline.AvailabilityZone)
	assert.Equal(t, "standard", baseline.VolumeType)
	assert.Equal(t, "[]", string(baseline.Attachments))
	assert.Equal(t, map[string]string{}, baseline.Metadata)
	const cinderTimestamp = "2006-01-02T15:04:05.000000"
	created, err := time.Parse(cinderTimestamp, baseline.CreatedAt)
	require.NoError(t, err)
	updated, err := time.Parse(cinderTimestamp, baseline.UpdatedAt)
	require.NoError(t, err)
	assert.False(t, updated.Before(created))
	assert.Contains(t, baseline.Links, volumeLink{Rel: "self", Href: instance.URL + collection + "/" + fixtureVolumeID})

	// https://docs.openstack.org/api-ref/block-storage/v3/#show-a-volume-s-details
	detail := collection + "/" + fixtureVolumeID
	status, headers, body = request(t, instance, http.MethodGet, detail, "", map[string]string{
		"X-Auth-Token":          provider.Token(),
		"OpenStack-API-Version": "volume 3.0",
	})

	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	assert.Equal(t, "volume 3.0", headers.Get("OpenStack-API-Version"))
	var shown struct {
		Volume wireVolume `json:"volume"`
	}
	require.NoError(t, json.Unmarshal(body, &shown))
	assert.Equal(t, baseline, shown.Volume)

	const missingID = "00000000-0000-0000-0000-000000000000"
	status, headers, body = request(t, instance, http.MethodGet, collection+"/"+missingID, "", auth)
	require.Equal(t, http.StatusNotFound, status, "%s", body)
	requireJSON(t, headers)
	var missing struct {
		Error struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"itemNotFound"`
	}
	require.NoError(t, json.Unmarshal(body, &missing))
	assert.Equal(t, http.StatusNotFound, missing.Error.Code)
	assert.NotEmpty(t, missing.Error.Message)

	status, _, body = request(t, instance, http.MethodGet, detail, "", nil)
	assert.Equal(t, http.StatusUnauthorized, status, "%s", body)
	status, _, body = request(t, instance, http.MethodGet, collection+"/detail", "", map[string]string{
		"X-Auth-Token": "invalid-token",
	})
	assert.Equal(t, http.StatusUnauthorized, status, "%s", body)
	status, _, body = request(t, instance, http.MethodGet, volumePath+"/"+missingID+"/volumes/detail", "", auth)
	assert.Equal(t, http.StatusBadRequest, status, "%s", body)
	status, _, body = request(t, instance, http.MethodGet, collection+"/detail?limit=-1", "", auth)
	assert.Equal(t, http.StatusBadRequest, status, "%s", body)
	status, _, body = request(t, instance, http.MethodGet, detail, "", map[string]string{
		"X-Auth-Token":          provider.Token(),
		"OpenStack-API-Version": "volume 3.4",
	})
	assert.Equal(t, http.StatusNotAcceptable, status, "%s", body)
}

func TestGophercloudDiscoversVolumeAndObservesFaultAndReset(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	discover := func(provider *gophercloud.ProviderClient) *gophercloud.ServiceClient {
		t.Helper()
		service, err := openstack.NewBlockStorageV3(provider, gophercloud.EndpointOpts{
			Region: "RegionOne", Availability: gophercloud.AvailabilityPublic,
		})
		require.NoError(t, err)
		assert.Equal(t, instance.URL+volumePath+"/"+volumeProjectID(t, provider)+"/", service.Endpoint)
		return service
	}
	service := discover(provider)
	list := func(client *gophercloud.ServiceClient) []volumes.Volume {
		t.Helper()
		pages, err := volumes.List(client, nil).AllPages(t.Context())
		require.NoError(t, err)
		items, err := volumes.ExtractVolumes(pages)
		require.NoError(t, err)
		return items
	}
	detail := func(client *gophercloud.ServiceClient, volumeID string) *volumes.Volume {
		t.Helper()
		volume, err := volumes.Get(t.Context(), client, volumeID).Extract()
		require.NoError(t, err)
		require.NotNil(t, volume)
		return volume
	}

	initial := list(service)
	require.Len(t, initial, 1)
	baseline := initial[0]
	require.NotEmpty(t, baseline.ID)
	assert.Equal(t, "available", baseline.Status)
	assert.Equal(t, 1, baseline.Size)
	assert.False(t, baseline.CreatedAt.IsZero())
	assert.Equal(t, baseline, *detail(service, baseline.ID))

	faultPath := "/_sandstack/volume/volumes/" + baseline.ID + "/fault"
	status, _, body := request(t, instance, http.MethodPut, faultPath, "", map[string]string{
		"X-SandStack-Admin-Token": provider.Token(),
	})

	require.Equal(t, http.StatusUnauthorized, status, "%s", body)
	assert.Equal(t, baseline, *detail(service, baseline.ID))

	admin := map[string]string{"X-SandStack-Admin-Token": adminToken}
	status, _, body = request(t, instance, http.MethodPut, faultPath, "", admin)

	require.Equal(t, http.StatusNoContent, status, "%s", body)
	faulted := detail(service, baseline.ID)
	assert.Equal(t, "error", faulted.Status)
	assert.Equal(t, baseline.ID, faulted.ID)
	assert.Equal(t, baseline.Name, faulted.Name)
	assert.Equal(t, baseline.Size, faulted.Size)
	assert.Equal(t, baseline.CreatedAt, faulted.CreatedAt)
	assert.Equal(t, []volumes.Volume{*faulted}, list(service))

	status, _, body = request(t, instance, http.MethodDelete, faultPath, "", admin)

	require.Equal(t, http.StatusNoContent, status, "%s", body)
	cleared := detail(service, baseline.ID)
	assert.Equal(t, "available", cleared.Status)
	assert.Equal(t, baseline.ID, cleared.ID)
	assert.Equal(t, []volumes.Volume{*cleared}, list(service))

	status, _, body = request(t, instance, http.MethodPut, faultPath, "", admin)
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	require.Equal(t, "error", detail(service, baseline.ID).Status)
	status, _, body = request(t, instance, http.MethodPost, "/_sandstack/reset", "", admin)

	require.Equal(t, http.StatusNoContent, status, "%s", body)
	_, err := volumes.Get(t.Context(), service, baseline.ID).Extract()
	require.Error(t, err)
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusUnauthorized), "%v", err)
	refreshed := discover(authenticate(t, instance))
	assert.Equal(t, baseline, *detail(refreshed, baseline.ID))
	assert.Equal(t, initial, list(refreshed))
}

func volumeProjectID(t *testing.T, provider *gophercloud.ProviderClient) string {
	t.Helper()
	token, ok := provider.GetAuthResult().(tokens.CreateResult)
	require.True(t, ok)
	project, err := token.ExtractProject()
	require.NoError(t, err)
	require.NotNil(t, project)
	require.NotEmpty(t, project.ID)
	return project.ID
}

type wireVolume struct {
	ID               string            `json:"id"`
	Name             string            `json:"name"`
	Size             int               `json:"size"`
	Status           string            `json:"status"`
	ProjectID        string            `json:"os-vol-tenant-attr:tenant_id"`
	CreatedAt        string            `json:"created_at"`
	UpdatedAt        string            `json:"updated_at"`
	AvailabilityZone string            `json:"availability_zone"`
	VolumeType       string            `json:"volume_type"`
	Attachments      json.RawMessage   `json:"attachments"`
	Metadata         map[string]string `json:"metadata"`
	Links            []volumeLink      `json:"links"`
}

type volumeLink struct {
	Rel  string `json:"rel"`
	Href string `json:"href"`
}
