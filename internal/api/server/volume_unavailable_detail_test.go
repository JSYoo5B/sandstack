package server_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/volumes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const unavailableVolumeDetailControl = "/_sandstack/volume/volumes/" + fixtureVolumeID + "/unavailable-detail"

func TestGophercloudRetriesCinderDetailAfterOneTransientFailure(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	client, err := openstack.NewBlockStorageV3(provider, gophercloud.EndpointOpts{
		Region: "RegionOne", Availability: gophercloud.AvailabilityPublic,
	})
	require.NoError(t, err)
	ctx := t.Context()
	baseline, err := volumes.Get(ctx, client, fixtureVolumeID).Extract()
	require.NoError(t, err)
	other, err := volumes.Create(ctx, client, volumes.CreateOpts{Size: 1, Name: "another-volume"}, nil).Extract()
	require.NoError(t, err)
	require.NotEqual(t, baseline.ID, other.ID)
	admin := map[string]string{"X-SandStack-Admin-Token": adminToken}
	for range 2 {
		status, _, body := request(t, instance, http.MethodPut, unavailableVolumeDetailControl, "", admin)
		require.Equal(t, http.StatusNoContent, status, "%s", body)
	}

	otherDetail, err := volumes.Get(ctx, client, other.ID).Extract()
	require.NoError(t, err)
	assert.Equal(t, other, otherDetail)
	pages, err := volumes.List(client, nil).AllPages(ctx)
	require.NoError(t, err)
	items, err := volumes.ExtractVolumes(pages)
	require.NoError(t, err)
	assert.ElementsMatch(t, []volumes.Volume{*baseline, *other}, items)
	detail := volumePath + "/" + volumeProjectID(t, provider) + "/volumes/" + fixtureVolumeID
	status, _, body := request(t, instance, http.MethodGet, detail, "", map[string]string{
		"X-Auth-Token": "invalid-token",
	})
	require.Equal(t, http.StatusUnauthorized, status, "%s", body)
	status, _, body = request(t, instance, http.MethodGet, detail, "", map[string]string{
		"X-Auth-Token": provider.Token(), "OpenStack-API-Version": "volume 3.4",
	})
	require.Equal(t, http.StatusNotAcceptable, status, "%s", body)
	name := "updated-while-detail-unavailable"
	updated, err := volumes.Update(ctx, client, fixtureVolumeID, volumes.UpdateOpts{Name: &name}).Extract()
	require.NoError(t, err)
	assert.Equal(t, name, updated.Name)
	assert.Equal(t, baseline.Status, updated.Status)

	_, err = volumes.Get(ctx, client, fixtureVolumeID).Extract()
	require.True(t, gophercloud.ResponseCodeIs(err, http.StatusServiceUnavailable), "%v", err)

	recovered, err := volumes.Get(ctx, client, fixtureVolumeID).Extract()
	require.NoError(t, err)
	assert.Equal(t, updated, recovered)
	repeated, err := volumes.Get(ctx, client, fixtureVolumeID).Extract()
	require.NoError(t, err)
	assert.Equal(t, recovered, repeated)
}

func TestCinderDetailTransientFailureWireAndControls(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	auth := map[string]string{"X-Auth-Token": provider.Token(), "OpenStack-API-Version": "volume 3.0"}
	admin := map[string]string{"X-SandStack-Admin-Token": adminToken}
	wrongAdmin := map[string]string{"X-SandStack-Admin-Token": provider.Token()}
	detail := volumePath + "/" + volumeProjectID(t, provider) + "/volumes/" + fixtureVolumeID
	status, headers, body := request(t, instance, http.MethodGet, detail, "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	baseline := string(body)

	status, _, body = request(t, instance, http.MethodPut, unavailableVolumeDetailControl, "", wrongAdmin)
	require.Equal(t, http.StatusUnauthorized, status, "%s", body)
	status, _, body = request(t, instance, http.MethodGet, detail, "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	assert.JSONEq(t, baseline, string(body))

	status, _, body = request(t, instance, http.MethodPut, unavailableVolumeDetailControl, "", admin)
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	status, _, body = request(t, instance, http.MethodDelete, unavailableVolumeDetailControl, "", wrongAdmin)
	require.Equal(t, http.StatusUnauthorized, status, "%s", body)

	// https://github.com/openstack/cinder/blob/master/cinder/api/openstack/wsgi.py#L1200-L1231
	status, headers, body = request(t, instance, http.MethodGet, detail, "", auth)
	require.Equal(t, http.StatusServiceUnavailable, status, "%s", body)
	requireJSON(t, headers)
	assert.Equal(t, "volume 3.0", headers.Get("OpenStack-API-Version"))
	var response struct {
		Fault struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"serviceUnavailable"`
		Volume json.RawMessage `json:"volume"`
	}
	require.NoError(t, json.Unmarshal(body, &response))
	assert.Equal(t, http.StatusServiceUnavailable, response.Fault.Code)
	assert.NotEmpty(t, response.Fault.Message)
	assert.Empty(t, response.Volume)
	status, headers, body = request(t, instance, http.MethodGet, detail, "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	assert.JSONEq(t, baseline, string(body))

	status, _, body = request(t, instance, http.MethodPut, unavailableVolumeDetailControl, "", admin)
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	for range 2 {
		status, _, body = request(t, instance, http.MethodDelete, unavailableVolumeDetailControl, "", admin)
		require.Equal(t, http.StatusNoContent, status, "%s", body)
	}
	status, _, body = request(t, instance, http.MethodGet, detail, "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	assert.JSONEq(t, baseline, string(body))

	const missingControl = "/_sandstack/volume/volumes/00000000-0000-0000-0000-000000000000/unavailable-detail"
	for _, method := range []string{http.MethodPut, http.MethodDelete} {
		status, _, body = request(t, instance, method, missingControl, "", admin)
		assert.Equal(t, http.StatusNotFound, status, "%s: %s", method, body)
	}

	status, _, body = request(t, instance, http.MethodPut, unavailableVolumeDetailControl, "", admin)
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	status, _, body = request(t, instance, http.MethodPost, "/_sandstack/reset", "", admin)
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	status, _, body = request(t, instance, http.MethodGet, detail, "", auth)
	require.Equal(t, http.StatusUnauthorized, status, "%s", body)
	fresh := authenticate(t, instance)
	auth["X-Auth-Token"] = fresh.Token()
	status, _, body = request(t, instance, http.MethodGet, detail, "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	assert.JSONEq(t, baseline, string(body))
}
