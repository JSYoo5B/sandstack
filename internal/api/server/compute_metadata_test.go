package server_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/servers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGophercloudReadsAndReplacesServerMetadata(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	compute, err := openstack.NewComputeV2(provider, gophercloud.EndpointOpts{
		Region: "RegionOne", Availability: gophercloud.AvailabilityPublic,
	})
	require.NoError(t, err)
	ctx := t.Context()
	admin := map[string]string{"X-SandStack-Admin-Token": adminToken}
	control := func(method, path string) {
		t.Helper()
		status, _, body := request(t, instance, method, path, "", admin)
		require.Equal(t, http.StatusNoContent, status, "%s", body)
	}
	read := func() map[string]string {
		t.Helper()
		metadata, err := servers.Metadata(ctx, compute, fixtureServerID).Extract()
		require.NoError(t, err)
		return metadata
	}
	replace := func(opts servers.MetadataOpts) {
		t.Helper()
		metadata, err := servers.ResetMetadata(ctx, compute, fixtureServerID, opts).Extract()
		require.NoError(t, err)
		assert.Equal(t, map[string]string(opts), metadata)
		assert.Equal(t, map[string]string(opts), read())
	}

	assert.Equal(t, map[string]string{}, read())
	initial := servers.MetadataOpts{"role": "old", "discard": "old"}
	replace(initial)
	const stale = "/_sandstack/compute/servers/" + fixtureServerID + "/stale-detail"
	control(http.MethodPut, stale)
	control(http.MethodPut, unavailableControl)

	latest := servers.MetadataOpts{"role": "new", "empty": "", "unicode": strings.Repeat("한", 255)}
	replace(latest)
	_, err = servers.ResetMetadata(ctx, compute, fixtureServerID, servers.MetadataOpts(nil)).Extract()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusBadRequest), "nil metadata: %v", err)
	assert.Equal(t, map[string]string(latest), read())

	active := listComputeServers(t, compute, "ACTIVE")
	require.Len(t, active, 1)
	assert.Equal(t, fixtureServerID, active[0].ID)
	assert.Equal(t, map[string]string(latest), active[0].Metadata)
	_, err = servers.Get(ctx, compute, fixtureServerID).Extract()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusServiceUnavailable), "detail outage remains pending: %v", err)
	detail, err := servers.Get(ctx, compute, fixtureServerID).Extract()
	require.NoError(t, err)
	assert.Equal(t, map[string]string(initial), detail.Metadata)
	control(http.MethodDelete, stale)

	require.NoError(t, servers.Stop(ctx, compute, fixtureServerID).ExtractErr())
	stopped := servers.MetadataOpts{"role": "while stopped"}
	replace(stopped)
	detail, err = servers.Get(ctx, compute, fixtureServerID).Extract()
	require.NoError(t, err)
	assert.Equal(t, "SHUTOFF", detail.Status)
	assert.Equal(t, map[string]string(stopped), detail.Metadata)

	control(http.MethodPut, faultPath)
	_, err = servers.ResetMetadata(ctx, compute, fixtureServerID, servers.MetadataOpts{}).Extract()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusConflict), "replace while in ERROR: %v", err)
	assert.Equal(t, map[string]string(stopped), read())
	control(http.MethodDelete, faultPath)

	replace(servers.MetadataOpts{})
	detail, err = servers.Get(ctx, compute, fixtureServerID).Extract()
	require.NoError(t, err)
	assert.Equal(t, "SHUTOFF", detail.Status)
	assert.Equal(t, map[string]string{}, detail.Metadata)
}

func TestNovaServerMetadataReplacementWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	base := computePath + "/" + routerProjectID(t, provider) + "/servers/"
	path := base + fixtureServerID + "/metadata"
	auth := map[string]string{"X-Auth-Token": provider.Token(), "OpenStack-API-Version": "compute 2.1"}
	const prior = `{"metadata":{"owner":"fixture team"}}`

	// https://docs.openstack.org/api-ref/compute/#replace-metadata-items
	// https://github.com/openstack/nova/blob/master/nova/api/openstack/compute/schemas/server_metadata.py#L43-L49
	status, headers, body := request(t, instance, http.MethodPut, path, prior, auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	assert.Equal(t, "compute 2.1", headers.Get("OpenStack-API-Version"))
	assert.JSONEq(t, prior, string(body))

	for _, invalid := range []string{
		`{}`,
		`{"metadata":null}`,
		`{"metadata":[]}`,
		`{"metadata":{"":"invalid"}}`,
		`{"metadata":{"scope/owner":"invalid"}}`,
		`{"metadata":{"owner":null}}`,
		`{"metadata":{"owner":1}}`,
		`{"metadata":{},"extra":true}`,
		fmt.Sprintf(`{"metadata":{%q:"long key"}}`, strings.Repeat("a", 256)),
		fmt.Sprintf(`{"metadata":{"long-value":%q}}`, strings.Repeat("한", 256)),
	} {
		status, headers, body := request(t, instance, http.MethodPut, path, invalid, auth)
		assert.Equal(t, http.StatusBadRequest, status, "%s: %s", invalid, body)
		requireJSON(t, headers)
	}
	status, _, body = request(t, instance, http.MethodGet, path, "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	assert.JSONEq(t, prior, string(body))

	for _, missing := range []string{"not-a-uuid", "00000000-0000-0000-0000-000000000000"} {
		status, _, body := request(t, instance, http.MethodGet, base+missing+"/metadata", "", auth)
		assert.Equal(t, http.StatusNotFound, status, "%s: %s", missing, body)
	}
	status, _, body = request(t, instance, http.MethodGet, path, "", nil)
	assert.Equal(t, http.StatusUnauthorized, status, "%s", body)

	compute, err := openstack.NewComputeV2(provider, gophercloud.EndpointOpts{
		Region: "RegionOne", Availability: gophercloud.AvailabilityPublic,
	})
	require.NoError(t, err)
	quota := servers.MetadataOpts{}
	for index := 0; index < 128; index++ {
		quota[fmt.Sprintf("key-%03d", index)] = ""
	}
	fields, err := servers.ResetMetadata(t.Context(), compute, fixtureServerID, quota).Extract()
	require.NoError(t, err)
	assert.Equal(t, map[string]string(quota), fields)

	quota["over-quota"] = "rejected"
	_, err = servers.ResetMetadata(t.Context(), compute, fixtureServerID, quota).Extract()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusForbidden), "metadata quota: %v", err)
	delete(quota, "over-quota")
	fields, err = servers.Metadata(t.Context(), compute, fixtureServerID).Extract()
	require.NoError(t, err)
	assert.Equal(t, map[string]string(quota), fields)
}
