package server_test

import (
	"net/http"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/servers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGophercloudObservesStaleComputeDetailAndLiveList(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	compute, err := openstack.NewComputeV2(provider, gophercloud.EndpointOpts{
		Region: "RegionOne", Availability: gophercloud.AvailabilityPublic,
	})
	require.NoError(t, err)
	initial := listComputeServers(t, compute, "")
	require.Len(t, initial, 1)
	serverID := initial[0].ID
	require.NotEmpty(t, serverID)
	readDetail := func() *servers.Server {
		t.Helper()
		detail, err := servers.Get(t.Context(), compute, serverID).Extract()
		require.NoError(t, err)
		return detail
	}

	baseline := readDetail()

	assert.Equal(t, initial[0], *baseline)
	assert.Equal(t, "ACTIVE", baseline.Status)
	assert.Equal(t, servers.Fault{}, baseline.Fault)
	assert.False(t, baseline.Created.IsZero())
	assert.False(t, baseline.Updated.IsZero())

	serverControl := "/_sandstack/compute/servers/" + serverID
	stalePath := serverControl + "/stale-detail"
	admin := map[string]string{"X-SandStack-Admin-Token": adminToken}
	status, _, body := request(t, instance, http.MethodPut, stalePath, "", admin)
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	status, _, body = request(t, instance, http.MethodPut, serverControl+"/fault", "", admin)
	require.Equal(t, http.StatusNoContent, status, "%s", body)

	live := listComputeServers(t, compute, "ERROR")

	require.Len(t, live, 1)
	assert.Equal(t, serverID, live[0].ID)
	assert.Equal(t, "ERROR", live[0].Status)
	assert.Equal(t, http.StatusInternalServerError, live[0].Fault.Code)
	assert.Equal(t, "Injected server fault", live[0].Fault.Message)
	assert.Equal(t, baseline, readDetail())

	status, _, body = request(t, instance, http.MethodPut, stalePath, "", admin)

	require.Equal(t, http.StatusNoContent, status, "%s", body)
	assert.Equal(t, baseline, readDetail())

	status, _, body = request(t, instance, http.MethodDelete, stalePath, "", map[string]string{
		"X-SandStack-Admin-Token": provider.Token(),
	})

	require.Equal(t, http.StatusUnauthorized, status, "%s", body)
	assert.Equal(t, baseline, readDetail())

	for range 2 {
		status, _, body = request(t, instance, http.MethodDelete, stalePath, "", admin)

		require.Equal(t, http.StatusNoContent, status, "%s", body)
		assert.Equal(t, live[0], *readDetail())
	}

	const missingPath = "/_sandstack/compute/servers/00000000-0000-0000-0000-000000000000/stale-detail"
	for _, method := range []string{http.MethodPut, http.MethodDelete} {
		status, _, body = request(t, instance, method, missingPath, "", admin)

		assert.Equal(t, http.StatusNotFound, status, "%s: %s", method, body)
	}
}
