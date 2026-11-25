package server_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/volumes"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/volumeattach"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGophercloudReplacesVolumeMetadataIndependentlyOfDetail(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	endpoint := gophercloud.EndpointOpts{Region: "RegionOne", Availability: gophercloud.AvailabilityPublic}
	storage, err := openstack.NewBlockStorageV3(provider, endpoint)
	require.NoError(t, err)
	compute, err := openstack.NewComputeV2(provider, endpoint)
	require.NoError(t, err)
	ctx := t.Context()
	attachment, err := volumeattach.Create(ctx, compute, fixtureServerID, volumeattach.CreateOpts{
		VolumeID: fixtureVolumeID,
	}).Extract()
	require.NoError(t, err)
	baseline, err := volumes.Get(ctx, storage, fixtureVolumeID).Extract()
	require.NoError(t, err)
	require.Equal(t, "in-use", baseline.Status)
	require.Len(t, baseline.Attachments, 1)
	require.Equal(t, map[string]string{}, baseline.Metadata)

	status, _, body := request(t, instance, http.MethodPut, unavailableVolumeDetailControl, "",
		map[string]string{"X-SandStack-Admin-Token": adminToken})
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	path := storage.ServiceURL("volumes", fixtureVolumeID, "metadata")
	type envelope struct {
		Metadata map[string]string `json:"metadata"`
	}
	read := func() map[string]string {
		t.Helper()
		var result envelope
		response, err := storage.Get(ctx, path, &result, nil)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, response.StatusCode)
		requireJSON(t, response.Header)
		assert.Empty(t, response.Header.Get("ETag"))
		require.NotNil(t, result.Metadata)
		return result.Metadata
	}
	replace := func(metadata map[string]string) {
		t.Helper()
		var result envelope
		response, err := storage.Put(ctx, path, envelope{Metadata: metadata}, &result,
			&gophercloud.RequestOpts{OkCodes: []int{http.StatusOK}})
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, response.StatusCode)
		requireJSON(t, response.Header)
		assert.Empty(t, response.Header.Get("ETag"))
		assert.Equal(t, metadata, result.Metadata)
		assert.Equal(t, metadata, read())
	}
	assert.Equal(t, map[string]string{}, read())
	replace(map[string]string{"scope/key": "old", "drop": "old", "empty-value": ""})
	fields := map[string]string{"scope/key": "replacement", "new": "value"}
	replace(fields)

	want := *baseline
	want.Metadata = fields
	pages, err := volumes.List(storage, nil).AllPages(ctx)
	require.NoError(t, err)
	listed, err := volumes.ExtractVolumes(pages)
	require.NoError(t, err)
	assert.Equal(t, []volumes.Volume{want}, listed)
	_, err = volumes.Get(ctx, storage, fixtureVolumeID).Extract()
	require.True(t, gophercloud.ResponseCodeIs(err, http.StatusServiceUnavailable), "the detail failure is still pending: %v", err)
	detail, err := volumes.Get(ctx, storage, fixtureVolumeID).Extract()
	require.NoError(t, err)
	assert.Equal(t, &want, detail)

	replace(map[string]string{})
	detail, err = volumes.Get(ctx, storage, fixtureVolumeID).Extract()
	require.NoError(t, err)
	assert.Equal(t, baseline, detail)
	retained, err := volumeattach.Get(ctx, compute, fixtureServerID, fixtureVolumeID).Extract()
	require.NoError(t, err)
	assert.Equal(t, attachment, retained)
}

func TestVolumeMetadataReplacementWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	path := volumePath + "/" + volumeProjectID(t, provider) + "/volumes/" + fixtureVolumeID + "/metadata"
	auth := map[string]string{"X-Auth-Token": provider.Token(), "OpenStack-API-Version": "volume 3.0"}
	check := func(method, payload, expected string) {
		t.Helper()
		status, headers, body := request(t, instance, method, path, payload, auth)
		require.Equal(t, http.StatusOK, status, "%s", body)
		requireJSON(t, headers)
		assert.Equal(t, "volume 3.0", headers.Get("OpenStack-API-Version"))
		assert.Empty(t, headers.Get("ETag"))
		assert.JSONEq(t, expected, string(body))
	}

	// https://docs.openstack.org/api-ref/block-storage/v3/#show-a-volume-s-metadata
	// https://docs.openstack.org/api-ref/block-storage/v3/#update-a-volume-s-metadata
	const empty = `{"metadata":{}}`
	const prior = `{"metadata":{"ref/path":"value","stable":"keep"}}`
	check(http.MethodGet, "", empty)
	check(http.MethodPut, prior, prior)
	for _, invalid := range []string{
		`{}`,
		`{"metadata":null}`,
		`{"metadata":[]}`,
		`{"metadata":{"item":null}}`,
		`{"metadata":{"item":12}}`,
		`{"metadata":{"stable":"must-not-change","stable":"duplicate"}}`,
		`{"metadata":{"stable":"must-not-change"},"extra":true}`,
		`{"metadata":{},"metadata":{}}`,
		`{"metadata":{"` + strings.Repeat("k", 256) + `":"too-long"}}`,
		`{"metadata":{"long":"` + strings.Repeat("v", 256) + `"}}`,
	} {
		status, headers, body := request(t, instance, http.MethodPut, path, invalid, auth)
		require.Equal(t, http.StatusBadRequest, status, "%s: %s", invalid, body)
		requireJSON(t, headers)
	}

	const missing = "00000000-0000-0000-0000-000000000000"
	const foreign = volumePath + "/" + missing + "/volumes/" + fixtureVolumeID + "/metadata"
	absent := volumePath + "/" + volumeProjectID(t, provider) + "/volumes/" + missing + "/metadata"
	for _, rejected := range []struct {
		method, path, payload string
		headers               map[string]string
		status                int
		kind                  string
	}{
		{http.MethodGet, path, "", nil, http.StatusUnauthorized, "unauthorized"},
		{http.MethodPut, path, empty, nil, http.StatusUnauthorized, "unauthorized"},
		{http.MethodGet, foreign, "", auth, http.StatusBadRequest, "badRequest"},
		{http.MethodPut, foreign, empty, auth, http.StatusBadRequest, "badRequest"},
		{http.MethodGet, absent, "", auth, http.StatusNotFound, "itemNotFound"},
		{http.MethodPut, absent, empty, auth, http.StatusNotFound, "itemNotFound"},
	} {
		status, headers, body := request(t, instance, rejected.method, rejected.path, rejected.payload, rejected.headers)
		require.Equal(t, rejected.status, status, "%s %s: %s", rejected.method, rejected.path, body)
		requireJSON(t, headers)
		var faults map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(body, &faults))
		assert.Contains(t, faults, rejected.kind)
	}
	check(http.MethodGet, "", prior)
	check(http.MethodPut, empty, empty)
	check(http.MethodGet, "", empty)
}
