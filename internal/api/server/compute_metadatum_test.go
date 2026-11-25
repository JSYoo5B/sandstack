package server_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/servers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGophercloudManagesNovaServerMetadataItems(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	compute, err := openstack.NewComputeV2(provider, gophercloud.EndpointOpts{
		Region: "RegionOne", Availability: gophercloud.AvailabilityPublic,
	})
	require.NoError(t, err)
	ctx := t.Context()
	initial := servers.MetadataOpts{"owner": "old", "retained": "keep"}
	fields, err := servers.ResetMetadata(ctx, compute, fixtureServerID, initial).Extract()
	require.NoError(t, err)
	require.Equal(t, map[string]string(initial), fields)

	item, err := servers.CreateMetadatum(ctx, compute, fixtureServerID,
		servers.MetadatumOpts{"owner": "new"}).Extract()
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"owner": "new"}, item)
	item, err = servers.CreateMetadatum(ctx, compute, fixtureServerID,
		servers.MetadatumOpts{"with space": ""}).Extract()
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"with space": ""}, item)
	item, err = servers.Metadatum(ctx, compute, fixtureServerID, "with space").Extract()
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"with space": ""}, item)

	before, err := servers.Get(ctx, compute, fixtureServerID).Extract()
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"owner": "new", "retained": "keep", "with space": ""}, before.Metadata)

	require.NoError(t, servers.DeleteMetadatum(ctx, compute, fixtureServerID, "with space").ExtractErr())

	after, err := servers.Get(ctx, compute, fixtureServerID).Extract()
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"owner": "new", "retained": "keep"}, after.Metadata)
	assert.Equal(t, before.Updated, after.Updated)
	_, err = servers.Metadatum(ctx, compute, fixtureServerID, "with space").Extract()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusNotFound), "read deleted item: %v", err)
	err = servers.DeleteMetadatum(ctx, compute, fixtureServerID, "with space").ExtractErr()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusNotFound), "delete missing item: %v", err)

	fields, err = servers.Metadata(ctx, compute, fixtureServerID).Extract()
	require.NoError(t, err)
	assert.Equal(t, after.Metadata, fields)

	atQuota := make(servers.MetadataOpts, 128)
	for index := 0; index < 128; index++ {
		atQuota[fmt.Sprintf("key-%03d", index)] = ""
	}
	_, err = servers.ResetMetadata(ctx, compute, fixtureServerID, atQuota).Extract()
	require.NoError(t, err)
	updated := servers.MetadatumOpts{"key-000": "overwrite"}
	item, err = servers.CreateMetadatum(ctx, compute, fixtureServerID, updated).Extract()
	require.NoError(t, err)
	assert.Equal(t, map[string]string(updated), item)
	_, err = servers.CreateMetadatum(ctx, compute, fixtureServerID,
		servers.MetadatumOpts{"new-key": "over quota"}).Extract()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusForbidden), "add item at quota: %v", err)
	atQuota["key-000"] = "overwrite"
	fields, err = servers.Metadata(ctx, compute, fixtureServerID).Extract()
	require.NoError(t, err)
	assert.Equal(t, map[string]string(atQuota), fields)
}

func TestNovaServerMetadataItemWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	path := computePath + "/" + routerProjectID(t, provider) + "/servers/" + fixtureServerID + "/metadata/"
	auth := map[string]string{"X-Auth-Token": provider.Token(), "OpenStack-API-Version": "compute 2.1"}
	checkItem := func(method, key, payload, expected string) {
		t.Helper()
		status, headers, body := request(t, instance, method, path+key, payload, auth)
		require.Equal(t, http.StatusOK, status, "%s", body)
		requireJSON(t, headers)
		assert.Equal(t, "compute 2.1", headers.Get("OpenStack-API-Version"))
		assert.JSONEq(t, expected, string(body))
	}

	// https://docs.openstack.org/api-ref/compute/#create-or-update-metadata-item
	// https://github.com/openstack/nova/blob/master/nova/api/openstack/compute/server_metadata.py#L74-L91
	const alpha = `{"meta":{"alpha":"one"}}`
	checkItem(http.MethodPut, "%61lpha", alpha, alpha)
	checkItem(http.MethodGet, "alpha", "", alpha)
	const space = `{"meta":{"with space":"two"}}`
	checkItem(http.MethodPut, "with%20space", space, space)
	checkItem(http.MethodGet, "with%20space", "", space)
	status, _, body := request(t, instance, http.MethodGet, path+"with%2520space", "", auth)
	assert.Equal(t, http.StatusNotFound, status, "%s", body)

	for _, invalid := range []string{
		`{"meta":{"other":"URL mismatch"}}`,
		`{"meta":{"alpha":"value","other":"extra"}}`,
		`{"meta":{}}`,
		`{"meta":{"alpha":null}}`,
		`{"metadata":{"alpha":"wrong wrapper"}}`,
	} {
		status, headers, body := request(t, instance, http.MethodPut, path+"alpha", invalid, auth)
		assert.Equal(t, http.StatusBadRequest, status, "%s: %s", invalid, body)
		requireJSON(t, headers)
	}
	checkItem(http.MethodGet, "alpha", "", alpha)

	status, _, body = request(t, instance, http.MethodPut, faultPath, "", map[string]string{
		"X-SandStack-Admin-Token": adminToken,
	})
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	status, _, body = request(t, instance, http.MethodDelete, path+"alpha", "", auth)
	assert.Equal(t, http.StatusConflict, status, "%s", body)
	status, _, body = request(t, instance, http.MethodDelete, path+"missing", "", auth)
	assert.Equal(t, http.StatusNotFound, status, "%s", body)
	checkItem(http.MethodGet, "alpha", "", alpha)

	status, _, body = request(t, instance, http.MethodDelete, faultPath, "", map[string]string{
		"X-SandStack-Admin-Token": adminToken,
	})
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	status, headers, body := request(t, instance, http.MethodDelete, path+"%61lpha", "", auth)
	assert.Equal(t, http.StatusNoContent, status, "%s", body)
	assert.Equal(t, "compute 2.1", headers.Get("OpenStack-API-Version"))
	assert.Empty(t, body)
	status, _, body = request(t, instance, http.MethodGet, path+"alpha", "", auth)
	assert.Equal(t, http.StatusNotFound, status, "%s", body)
	checkItem(http.MethodGet, "with%20space", "", space)
}
