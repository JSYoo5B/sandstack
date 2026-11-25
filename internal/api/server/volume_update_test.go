package server_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/volumes"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/volumeattach"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGophercloudUpdatesAttachedVolumeMetadata(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	endpoint := gophercloud.EndpointOpts{Region: "RegionOne", Availability: gophercloud.AvailabilityPublic}
	storage, err := openstack.NewBlockStorageV3(provider, endpoint)
	require.NoError(t, err)
	compute, err := openstack.NewComputeV2(provider, endpoint)
	require.NoError(t, err)
	ctx := t.Context()
	association, err := volumeattach.Create(ctx, compute, fixtureServerID, volumeattach.CreateOpts{
		VolumeID: fixtureVolumeID,
	}).Extract()
	require.NoError(t, err)
	baseline, err := volumes.Get(ctx, storage, fixtureVolumeID).Extract()
	require.NoError(t, err)
	require.Equal(t, "in-use", baseline.Status)
	require.Len(t, baseline.Attachments, 1)
	require.NotEmpty(t, baseline.Attachments[0].AttachmentID)
	check := func(want, got *volumes.Volume) {
		t.Helper()
		require.NotNil(t, got)
		assert.False(t, got.UpdatedAt.Before(want.UpdatedAt))
		expected := *want
		expected.UpdatedAt = got.UpdatedAt
		assert.Equal(t, &expected, got)
	}
	checkReads := func(want *volumes.Volume) {
		t.Helper()
		detail, err := volumes.Get(ctx, storage, fixtureVolumeID).Extract()
		require.NoError(t, err)
		assert.Equal(t, want, detail)
		pages, err := volumes.List(storage, nil).AllPages(ctx)
		require.NoError(t, err)
		listed, err := volumes.ExtractVolumes(pages)
		require.NoError(t, err)
		require.Len(t, listed, 1)
		assert.Equal(t, *want, listed[0])
	}
	description := "metadata update with an existing attachment"
	described, err := volumes.Update(ctx, storage, fixtureVolumeID, volumes.UpdateOpts{
		Description: &description,
	}).Extract()

	require.NoError(t, err)
	expected := *baseline
	expected.Description = description
	check(&expected, described)
	checkReads(described)

	name := "renamed-attached-volume"
	renamed, err := volumes.Update(ctx, storage, fixtureVolumeID, volumes.UpdateOpts{Name: &name}).Extract()

	require.NoError(t, err)
	expected = *described
	expected.Name = name
	check(&expected, renamed)
	checkReads(renamed)

	empty := ""
	cleared, err := volumes.Update(ctx, storage, fixtureVolumeID, volumes.UpdateOpts{
		Name: &empty, Description: &empty,
	}).Extract()

	require.NoError(t, err)
	expected = *renamed
	expected.Name, expected.Description = "", ""
	check(&expected, cleared)
	checkReads(cleared)
	stillAttached, err := volumeattach.Get(ctx, compute, fixtureServerID, fixtureVolumeID).Extract()
	require.NoError(t, err)
	assert.Equal(t, association, stillAttached)

	// The SDK decodes both null and empty JSON strings as the empty Go string.
	path := volumePath + "/" + volumeProjectID(t, provider) + "/volumes/" + fixtureVolumeID
	status, headers, body := request(t, instance, http.MethodGet, path, "", map[string]string{
		"X-Auth-Token": provider.Token(),
	})
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	var response struct {
		Volume map[string]json.RawMessage `json:"volume"`
	}
	require.NoError(t, json.Unmarshal(body, &response))
	assert.JSONEq(t, `""`, string(response.Volume["name"]))
	assert.JSONEq(t, `""`, string(response.Volume["description"]))
}

func TestVolumeMetadataUpdateWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	projectID := volumeProjectID(t, provider)
	collection := volumePath + "/" + projectID + "/volumes"
	detail := collection + "/" + fixtureVolumeID
	auth := map[string]string{"X-Auth-Token": provider.Token(), "OpenStack-API-Version": "volume 3.0"}
	read := func(body []byte) map[string]json.RawMessage {
		t.Helper()
		var response struct {
			Volume map[string]json.RawMessage `json:"volume"`
		}
		require.NoError(t, json.Unmarshal(body, &response))
		require.Equal(t, `"`+fixtureVolumeID+`"`, string(response.Volume["id"]))
		return response.Volume
	}
	checkFault := func(body []byte, kind string, code int) {
		t.Helper()
		var response map[string]struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		}
		require.NoError(t, json.Unmarshal(body, &response))
		require.Contains(t, response, kind)
		assert.Equal(t, code, response[kind].Code)
		assert.NotEmpty(t, response[kind].Message)
	}
	const setText = `{"volume":{"name":"new-volume","description":"keep-description"}}`
	status, headers, body := request(t, instance, http.MethodPut, detail, setText, nil)
	require.Equal(t, http.StatusUnauthorized, status, "%s", body)
	requireJSON(t, headers)
	checkFault(body, "unauthorized", status)

	// https://docs.openstack.org/api-ref/block-storage/v3/#update-a-volume
	status, headers, body = request(t, instance, http.MethodPut, detail, setText, auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	assert.Equal(t, "volume 3.0", headers.Get("OpenStack-API-Version"))
	updated := read(body)
	assert.JSONEq(t, `"new-volume"`, string(updated["name"]))
	assert.JSONEq(t, `"keep-description"`, string(updated["description"]))
	assert.JSONEq(t, `"available"`, string(updated["status"]))
	assert.JSONEq(t, "1", string(updated["size"]))
	assert.JSONEq(t, "[]", string(updated["attachments"]))
	assert.JSONEq(t, "{}", string(updated["metadata"]))

	status, headers, body = request(t, instance, http.MethodPut, detail, `{"volume":{"name":null}}`, auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	nameCleared := read(body)
	assert.JSONEq(t, "null", string(nameCleared["name"]))
	assert.JSONEq(t, `"keep-description"`, string(nameCleared["description"]))

	status, headers, body = request(t, instance, http.MethodPut, detail, `{"volume":{"description":null}}`, auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	cleared := read(body)
	assert.JSONEq(t, "null", string(cleared["name"]))
	assert.JSONEq(t, "null", string(cleared["description"]))

	status, headers, body = request(t, instance, http.MethodPut, detail, `{"volume":{}}`, auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	assert.Equal(t, cleared, read(body))
	expected := string(body)

	for _, invalid := range []string{
		`{}`,
		`{"volume":null}`,
		`{"volume":{"name":23}}`,
		`{"volume":{"description":[]}}`,
		`{"volume":{"name":"one","name":"two"}}`,
		`{"volume":{"name":"must-not-change","size":3}}`,
		`{"volume":{"status":"in-use"}}`,
		`{"volume":{"metadata":{"test":"unsupported"}}}`,
	} {
		status, headers, body := request(t, instance, http.MethodPut, detail, invalid, auth)
		require.Equal(t, http.StatusBadRequest, status, "%s: %s", invalid, body)
		requireJSON(t, headers)
		checkFault(body, "badRequest", status)
	}
	status, headers, body = request(t, instance, http.MethodGet, detail, "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	assert.JSONEq(t, expected, string(body))

	const missing = "00000000-0000-0000-0000-000000000000"
	status, headers, body = request(t, instance, http.MethodPut, collection+"/"+missing, setText, auth)
	require.Equal(t, http.StatusNotFound, status, "%s", body)
	requireJSON(t, headers)
	checkFault(body, "itemNotFound", status)
}
