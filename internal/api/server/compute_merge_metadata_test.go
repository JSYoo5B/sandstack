package server_test

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/servers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGophercloudMergesServerMetadataAndChecksResultingQuota(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	compute, err := openstack.NewComputeV2(provider, gophercloud.EndpointOpts{
		Region: "RegionOne", Availability: gophercloud.AvailabilityPublic,
	})
	require.NoError(t, err)
	ctx := t.Context()
	seed := servers.MetadataOpts{"keep": "unchanged", "revise": "old"}
	for i := range 125 {
		seed[fmt.Sprintf("key-%03d", i)] = "original"
	}
	baseline, err := servers.ResetMetadata(ctx, compute, fixtureServerID, seed).Extract()
	require.NoError(t, err)
	require.Equal(t, map[string]string(seed), baseline)
	read := func() map[string]string {
		t.Helper()
		current, err := servers.Metadata(ctx, compute, fixtureServerID).Extract()
		require.NoError(t, err)
		return current
	}

	merged, err := servers.UpdateMetadata(ctx, compute, fixtureServerID, servers.MetadataOpts{
		"revise": "new", "added": "",
	}).Extract()

	require.NoError(t, err)
	want := maps.Clone(baseline)
	want["revise"], want["added"] = "new", ""
	assert.Len(t, merged, 128)
	assert.Equal(t, want, merged)
	assert.Equal(t, want, read())
	atQuota, err := servers.UpdateMetadata(ctx, compute, fixtureServerID, servers.MetadataOpts{
		"revise": "at-quota",
	}).Extract()
	require.NoError(t, err)
	want["revise"] = "at-quota"
	assert.Equal(t, want, atQuota)

	_, err = servers.UpdateMetadata(ctx, compute, fixtureServerID, servers.MetadataOpts{
		"revise": "must not change", "over-quota": "new-key",
	}).Extract()
	require.True(t, gophercloud.ResponseCodeIs(err, http.StatusForbidden), "resulting map exceeds quota: %v", err)
	assert.Equal(t, want, read())
	detail, err := servers.Get(ctx, compute, fixtureServerID).Extract()
	require.NoError(t, err)
	assert.Equal(t, "ACTIVE", detail.Status)
	assert.Equal(t, want, detail.Metadata)
	assert.Equal(t, []servers.Server{*detail}, listComputeServers(t, compute, "ACTIVE"))

	unchanged, err := servers.UpdateMetadata(ctx, compute, fixtureServerID, servers.MetadataOpts{}).Extract()
	require.NoError(t, err)
	assert.Equal(t, want, unchanged)
	afterNoop, err := servers.Get(ctx, compute, fixtureServerID).Extract()
	require.NoError(t, err)
	assert.Equal(t, detail, afterNoop)

	status, _, body := request(t, instance, http.MethodPut, faultPath, "",
		map[string]string{"X-SandStack-Admin-Token": adminToken})
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	_, err = servers.UpdateMetadata(ctx, compute, fixtureServerID, servers.MetadataOpts{
		"revise": "must not change",
	}).Extract()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusConflict), "merge in ERROR: %v", err)
	assert.Equal(t, want, read())
}

func TestNovaMergeServerMetadataWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	base := computePath + "/" + routerProjectID(t, provider) + "/servers/"
	path := base + fixtureServerID + "/metadata"
	auth := map[string]string{"X-Auth-Token": provider.Token(), "OpenStack-API-Version": "compute 2.1"}
	const seed = `{"metadata":{"keep":"original","revise":"old"}}`
	status, _, body := request(t, instance, http.MethodPut, path, seed, auth)
	require.Equal(t, http.StatusOK, status, "%s", body)

	// https://docs.openstack.org/api-ref/compute/#create-or-update-server-metadata
	const patch = `{"metadata":{"revise":"updated","added":"value"}}`
	const expected = `{"metadata":{"keep":"original","revise":"updated","added":"value"}}`
	status, headers, body := request(t, instance, http.MethodPost, path, patch, auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	assert.Equal(t, "compute 2.1", headers.Get("OpenStack-API-Version"))
	assert.JSONEq(t, expected, string(body))

	for _, invalid := range []string{
		`{"metadata":null}`,
		`{"metadata":{"revise":"must not change","added":null}}`,
		`{"metadata":{"revise":"must not change","added":5}}`,
		`{"metadata":{"revise":"must not change","revise":"duplicate"}}`,
		`{"metadata":{"revise":"must not change"},"extra":true}`,
	} {
		status, headers, body := request(t, instance, http.MethodPost, path, invalid, auth)
		require.Equal(t, http.StatusBadRequest, status, "%s: %s", invalid, body)
		requireJSON(t, headers)
		var fault map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(body, &fault))
		assert.Contains(t, fault, "badRequest")
	}
	status, _, body = request(t, instance, http.MethodPost, path, patch, nil)
	require.Equal(t, http.StatusUnauthorized, status, "%s", body)
	const missing = "00000000-0000-0000-0000-000000000000"
	status, headers, body = request(t, instance, http.MethodPost, base+missing+"/metadata", patch, auth)
	require.Equal(t, http.StatusNotFound, status, "%s", body)
	requireJSON(t, headers)
	status, headers, body = request(t, instance, http.MethodGet, path, "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	assert.JSONEq(t, expected, string(body))
}
