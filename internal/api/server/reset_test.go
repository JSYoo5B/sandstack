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

func TestSandboxResetRestoresSeedAndInvalidatesTokens(t *testing.T) {
	const resetPath = "/_sandstack/reset"
	instance := startServer(t, "")
	newCompute := func(provider *gophercloud.ProviderClient) *gophercloud.ServiceClient {
		t.Helper()
		client, err := openstack.NewComputeV2(provider, gophercloud.EndpointOpts{
			Region: "RegionOne", Availability: gophercloud.AvailabilityPublic,
		})
		require.NoError(t, err)
		return client
	}
	provider := authenticate(t, instance)
	compute := newCompute(provider)
	identity := identityClient(t, authenticate(t, instance), instance.URL)
	checkClientCatalog(t, identity, instance.URL)
	seed := listComputeServers(t, compute, "")
	require.Len(t, seed, 1)
	serverID := seed[0].ID
	require.NotEmpty(t, serverID)
	readDetail := func(client *gophercloud.ServiceClient) *servers.Server {
		t.Helper()
		detail, err := servers.Get(t.Context(), client, serverID).Extract()
		require.NoError(t, err)
		return detail
	}
	baseline := readDetail(compute)
	assert.Equal(t, seed[0], *baseline)
	assert.Equal(t, "ACTIVE", baseline.Status)
	assert.Equal(t, servers.Fault{}, baseline.Fault)
	assert.False(t, baseline.Created.IsZero())
	assert.False(t, baseline.Updated.IsZero())

	admin := map[string]string{"X-SandStack-Admin-Token": adminToken}
	serverControl := "/_sandstack/compute/servers/" + serverID
	faultControl := serverControl + "/fault"
	status, _, body := request(t, instance, http.MethodPut, faultControl, "", admin)
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	faulted := readDetail(compute)
	require.Equal(t, "ERROR", faulted.Status)
	assert.Equal(t, http.StatusInternalServerError, faulted.Fault.Code)
	status, _, body = request(t, instance, http.MethodPut, serverControl+"/stale-detail", "", admin)
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	status, _, body = request(t, instance, http.MethodDelete, faultControl, "", admin)
	require.Equal(t, http.StatusNoContent, status, "%s", body)

	live := listComputeServers(t, compute, "ACTIVE")
	require.Len(t, live, 1)
	assert.Equal(t, servers.Fault{}, live[0].Fault)
	assert.Equal(t, faulted, readDetail(compute))

	for _, credentials := range []map[string]string{nil, {"X-SandStack-Admin-Token": provider.Token()}} {
		status, _, body = request(t, instance, http.MethodPost, resetPath, "", credentials)

		require.Equal(t, http.StatusUnauthorized, status, "%s", body)
		checkClientCatalog(t, identity, instance.URL)
		assert.Equal(t, live, listComputeServers(t, compute, ""))
		assert.Equal(t, faulted, readDetail(compute))
	}

	// Leave both the live fault and the older detail snapshot for reset to clear.
	status, _, body = request(t, instance, http.MethodPut, faultControl, "", admin)
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	require.Len(t, listComputeServers(t, compute, "ERROR"), 1)
	assert.Equal(t, faulted, readDetail(compute))

	status, _, body = request(t, instance, http.MethodPost, resetPath, "", admin)

	require.Equal(t, http.StatusNoContent, status, "%s", body)
	assertClientUnauthorized(t, identity)
	_, err := servers.Get(t.Context(), compute, serverID).Extract()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusUnauthorized), "%v", err)

	fresh := authenticate(t, instance)
	assert.NotEqual(t, provider.Token(), fresh.Token())
	freshCompute := newCompute(fresh)
	assert.Equal(t, baseline, readDetail(freshCompute))
	assert.Equal(t, seed, listComputeServers(t, freshCompute, ""))

	status, _, body = request(t, instance, http.MethodPost, resetPath, "", admin)

	require.Equal(t, http.StatusNoContent, status, "%s", body)
	_, err = servers.Get(t.Context(), freshCompute, serverID).Extract()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusUnauthorized), "%v", err)
	assert.Equal(t, seed, listComputeServers(t, newCompute(authenticate(t, instance)), ""))
}
