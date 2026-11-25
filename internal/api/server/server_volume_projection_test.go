package server_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/servers"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/volumeattach"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerAttachedVolumeProjectionWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	computeURL := computePath + "/" + volumeProjectID(t, provider)
	checkProjection := func(expected string) {
		t.Helper()
		type serverProjection struct {
			ID      string          `json:"id"`
			Volumes json.RawMessage `json:"os-extended-volumes:volumes_attached"`
		}
		// https://docs.openstack.org/api-ref/compute/#show-server-details
		status, headers, body := request(t, instance, http.MethodGet, computeURL+"/servers/"+fixtureServerID, "", auth)
		require.Equal(t, http.StatusOK, status, "%s", body)
		requireJSON(t, headers)
		var detail struct {
			Server serverProjection `json:"server"`
		}
		require.NoError(t, json.Unmarshal(body, &detail))
		assert.Equal(t, fixtureServerID, detail.Server.ID)
		assert.JSONEq(t, expected, string(detail.Server.Volumes))

		// https://docs.openstack.org/api-ref/compute/#list-servers-detailed
		status, headers, body = request(t, instance, http.MethodGet, computeURL+"/servers/detail", "", auth)
		require.Equal(t, http.StatusOK, status, "%s", body)
		requireJSON(t, headers)
		var listing struct {
			Servers []serverProjection `json:"servers"`
		}
		require.NoError(t, json.Unmarshal(body, &listing))
		require.Len(t, listing.Servers, 1)
		assert.Equal(t, fixtureServerID, listing.Servers[0].ID)
		assert.JSONEq(t, expected, string(listing.Servers[0].Volumes))
	}
	checkProjection("[]")

	const payload = `{"volumeAttachment":{"volumeId":"` + fixtureVolumeID + `"}}`
	status, _, body := request(t, instance, http.MethodPost, computeURL+fixtureAttachmentPath, payload, auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	checkProjection(`[{"id":"` + fixtureVolumeID + `"}]`)
}

func TestGophercloudReadsStaleAndLiveServerAttachments(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	discover := func(provider *gophercloud.ProviderClient) *gophercloud.ServiceClient {
		t.Helper()
		compute, err := openstack.NewComputeV2(provider, gophercloud.EndpointOpts{
			Region: "RegionOne", Availability: gophercloud.AvailabilityPublic,
		})
		require.NoError(t, err)
		return compute
	}
	compute := discover(provider)
	assertDetail := func(client *gophercloud.ServiceClient, want []servers.AttachedVolume) {
		t.Helper()
		result, err := servers.Get(t.Context(), client, fixtureServerID).Extract()
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, want, result.AttachedVolumes)
	}
	assertList := func(client *gophercloud.ServiceClient, want []servers.AttachedVolume) {
		t.Helper()
		listing := listComputeServers(t, client, "")
		require.Len(t, listing, 1)
		assert.Equal(t, fixtureServerID, listing[0].ID)
		assert.Equal(t, want, listing[0].AttachedVolumes)
	}
	admin := map[string]string{"X-SandStack-Admin-Token": adminToken}
	control := func(method, path string) {
		t.Helper()
		status, _, body := request(t, instance, method, path, "", admin)
		require.Equal(t, http.StatusNoContent, status, "%s", body)
	}
	const stale = "/_sandstack/compute/servers/" + fixtureServerID + "/stale-detail"
	empty := []servers.AttachedVolume{}
	attached := []servers.AttachedVolume{{ID: fixtureVolumeID}}
	assertDetail(compute, empty)
	assertList(compute, empty)

	control(http.MethodPut, stale)
	_, err := volumeattach.Create(t.Context(), compute, fixtureServerID, volumeattach.CreateOpts{
		VolumeID: fixtureVolumeID,
	}).Extract()
	require.NoError(t, err)
	assertDetail(compute, empty)
	assertList(compute, attached)

	control(http.MethodPut, unavailableControl)
	assertList(compute, attached)
	_, err = servers.Get(t.Context(), compute, fixtureServerID).Extract()
	require.Error(t, err)
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusServiceUnavailable), "%v", err)
	assertDetail(compute, empty)
	control(http.MethodDelete, stale)
	assertDetail(compute, attached)

	control(http.MethodPut, stale)
	require.NoError(t, volumeattach.Delete(t.Context(), compute, fixtureServerID, fixtureVolumeID).ExtractErr())
	assertList(compute, empty)
	assertDetail(compute, attached)

	control(http.MethodPost, "/_sandstack/reset")
	_, err = servers.Get(t.Context(), compute, fixtureServerID).Extract()
	require.Error(t, err)
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusUnauthorized), "%v", err)
	current := discover(authenticate(t, instance))
	assertDetail(current, empty)
	assertList(current, empty)
}
