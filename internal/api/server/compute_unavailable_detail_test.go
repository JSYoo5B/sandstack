package server_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/servers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const unavailableControl = "/_sandstack/compute/servers/" + fixtureServerID + "/unavailable-detail"

func TestGophercloudCanRetryComputeDetailAfterOneTransientFailure(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	token := provider.Token()
	compute, err := openstack.NewComputeV2(provider, gophercloud.EndpointOpts{
		Region: "RegionOne", Availability: gophercloud.AvailabilityPublic,
	})
	require.NoError(t, err)
	baseline, err := servers.Get(t.Context(), compute, fixtureServerID).Extract()
	require.NoError(t, err)
	require.NotEmpty(t, baseline.TenantID)
	require.Equal(t, "ACTIVE", baseline.Status)

	admin := map[string]string{"X-SandStack-Admin-Token": adminToken}
	for range 2 {
		status, _, body := request(t, instance, http.MethodPut, unavailableControl, "", admin)
		require.Equal(t, http.StatusNoContent, status, "%s", body)
	}

	prefix := computePath + "/" + baseline.TenantID + "/servers/"
	status, _, body := request(t, instance, http.MethodGet, prefix+fixtureServerID, "", map[string]string{
		"X-Auth-Token": "invalid-token",
	})
	require.Equal(t, http.StatusUnauthorized, status, "%s", body)
	auth := map[string]string{"X-Auth-Token": token}
	wrongScope := computePath + "/another-project/servers/" + fixtureServerID
	status, _, body = request(t, instance, http.MethodGet, wrongScope, "", auth)
	require.Equal(t, http.StatusBadRequest, status, "%s", body)
	const missingID = "00000000-0000-0000-0000-000000000000"
	status, _, body = request(t, instance, http.MethodGet, prefix+missingID, "", auth)
	require.Equal(t, http.StatusNotFound, status, "%s", body)
	assert.Equal(t, []servers.Server{*baseline}, listComputeServers(t, compute, ""))

	_, err = servers.Get(t.Context(), compute, fixtureServerID).Extract()

	require.True(t, gophercloud.ResponseCodeIs(err, http.StatusServiceUnavailable), "%v", err)

	recovered, err := servers.Get(t.Context(), compute, fixtureServerID).Extract()

	require.NoError(t, err)
	assert.Equal(t, baseline, recovered)
	assert.Equal(t, token, provider.Token())

	status, _, body = request(t, instance, http.MethodGet, "/_sandstack/requests", "", admin)
	require.Equal(t, http.StatusOK, status, "%s", body)
	var journal struct {
		Requests []struct {
			Method string `json:"method"`
			Route  string `json:"route"`
			Status int    `json:"status"`
		} `json:"requests"`
	}
	require.NoError(t, json.Unmarshal(body, &journal))
	require.GreaterOrEqual(t, len(journal.Requests), 2)
	failed := journal.Requests[len(journal.Requests)-2]
	retried := journal.Requests[len(journal.Requests)-1]
	assert.Equal(t, http.MethodGet, failed.Method)
	assert.Equal(t, computePath+"/{projectID}/servers/{serverID}", failed.Route)
	assert.Equal(t, http.StatusServiceUnavailable, failed.Status)
	assert.Equal(t, failed.Method, retried.Method)
	assert.Equal(t, failed.Route, retried.Route)
	assert.Equal(t, http.StatusOK, retried.Status)
}

func TestComputeTransientFailureWireAndControls(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	detailPath := computePath + "/servers/" + fixtureServerID
	status, _, body := request(t, instance, http.MethodGet, detailPath, "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	baseline := decodeWireComputeServer(t, body)
	require.Equal(t, fixtureServerID, baseline.ID)
	require.Equal(t, "ACTIVE", baseline.Status)

	admin := map[string]string{"X-SandStack-Admin-Token": adminToken}
	wrongAdmin := map[string]string{"X-SandStack-Admin-Token": provider.Token()}
	status, _, body = request(t, instance, http.MethodPut, unavailableControl, "", wrongAdmin)
	require.Equal(t, http.StatusUnauthorized, status, "%s", body)
	status, _, body = request(t, instance, http.MethodGet, detailPath, "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	assert.Equal(t, baseline, decodeWireComputeServer(t, body))

	status, _, body = request(t, instance, http.MethodPut, unavailableControl, "", admin)
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	status, _, body = request(t, instance, http.MethodDelete, unavailableControl, "", wrongAdmin)
	require.Equal(t, http.StatusUnauthorized, status, "%s", body)

	// https://github.com/openstack/nova/blob/master/nova/api/openstack/wsgi.py
	status, headers, body := request(t, instance, http.MethodGet, detailPath, "", auth)

	require.Equal(t, http.StatusServiceUnavailable, status, "%s", body)
	requireJSON(t, headers)
	var response struct {
		Fault struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"serviceUnavailable"`
		Server json.RawMessage `json:"server"`
	}
	require.NoError(t, json.Unmarshal(body, &response))
	assert.Equal(t, http.StatusServiceUnavailable, response.Fault.Code)
	assert.NotEmpty(t, response.Fault.Message)
	assert.Empty(t, response.Server)

	status, _, body = request(t, instance, http.MethodGet, detailPath, "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	assert.Equal(t, baseline, decodeWireComputeServer(t, body))

	status, _, body = request(t, instance, http.MethodPut, unavailableControl, "", admin)
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	for range 2 {
		status, _, body = request(t, instance, http.MethodDelete, unavailableControl, "", admin)
		require.Equal(t, http.StatusNoContent, status, "%s", body)
	}
	status, _, body = request(t, instance, http.MethodGet, detailPath, "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	assert.Equal(t, baseline, decodeWireComputeServer(t, body))

	const missingControl = "/_sandstack/compute/servers/00000000-0000-0000-0000-000000000000/unavailable-detail"
	for _, method := range []string{http.MethodPut, http.MethodDelete} {
		status, _, body = request(t, instance, method, missingControl, "", admin)
		assert.Equal(t, http.StatusNotFound, status, "%s: %s", method, body)
	}

	status, _, body = request(t, instance, http.MethodPut, unavailableControl, "", admin)
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	status, _, body = request(t, instance, http.MethodPost, "/_sandstack/reset", "", admin)
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	status, _, body = request(t, instance, http.MethodGet, detailPath, "", auth)
	require.Equal(t, http.StatusUnauthorized, status, "%s", body)

	fresh := authenticate(t, instance)
	status, _, body = request(t, instance, http.MethodGet, detailPath, "", map[string]string{
		"X-Auth-Token": fresh.Token(),
	})
	require.Equal(t, http.StatusOK, status, "%s", body)
	assert.Equal(t, baseline, decodeWireComputeServer(t, body))
}
