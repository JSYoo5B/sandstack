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

func TestGophercloudMergesVolumeMetadataWithoutReplacingOtherKeys(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	client, err := openstack.NewBlockStorageV3(provider, gophercloud.EndpointOpts{
		Region: "RegionOne", Availability: gophercloud.AvailabilityPublic,
	})
	require.NoError(t, err)
	ctx := t.Context()
	type envelope struct {
		Metadata map[string]string `json:"metadata"`
	}
	url := client.ServiceURL("volumes", fixtureVolumeID, "metadata")
	ok := &gophercloud.RequestOpts{OkCodes: []int{http.StatusOK}}
	seed := envelope{Metadata: map[string]string{"scope/key": "unchanged", "revise": "old"}}
	var initial envelope
	_, err = client.Put(ctx, url, seed, &initial, ok)
	require.NoError(t, err)
	require.Equal(t, seed, initial)
	baseline, err := volumes.Get(ctx, client, fixtureVolumeID).Extract()
	require.NoError(t, err)
	require.Equal(t, "available", baseline.Status)
	require.Equal(t, seed.Metadata, baseline.Metadata)

	var merged envelope
	response, err := client.Post(ctx, url, envelope{Metadata: map[string]string{"revise": "new", "added/key": ""}},
		&merged, ok)

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, response.StatusCode)
	requireJSON(t, response.Header)
	assert.Empty(t, response.Header.Get("ETag"))
	want := *baseline
	want.Metadata = map[string]string{"scope/key": "unchanged", "revise": "new", "added/key": ""}
	assert.Equal(t, want.Metadata, merged.Metadata)
	var current envelope
	_, err = client.Get(ctx, url, &current, nil)
	require.NoError(t, err)
	assert.Equal(t, merged, current)
	detail, err := volumes.Get(ctx, client, fixtureVolumeID).Extract()
	require.NoError(t, err)
	assert.Equal(t, &want, detail)
	pages, err := volumes.List(client, nil).AllPages(ctx)
	require.NoError(t, err)
	listed, err := volumes.ExtractVolumes(pages)
	require.NoError(t, err)
	assert.Equal(t, []volumes.Volume{want}, listed)
}

func TestVolumeMergeMetadataWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	collection := volumePath + "/" + volumeProjectID(t, provider) + "/volumes"
	detail := collection + "/" + fixtureVolumeID
	url := detail + "/metadata"
	auth := map[string]string{"X-Auth-Token": provider.Token(), "OpenStack-API-Version": "volume 3.0"}
	const seed = `{"metadata":{"ref/path":"preserved","revise":"old"}}`
	status, headers, body := request(t, instance, http.MethodPut, url, seed, auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)

	// https://docs.openstack.org/api-ref/block-storage/v3/#create-a-volume-s-metadata
	const patch = `{"metadata":{"revise":"new","added":"yes"}}`
	const expected = `{"metadata":{"ref/path":"preserved","revise":"new","added":"yes"}}`
	status, headers, body = request(t, instance, http.MethodPost, url, patch, auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	assert.Equal(t, "volume 3.0", headers.Get("OpenStack-API-Version"))
	assert.Empty(t, headers.Get("ETag"))
	assert.JSONEq(t, expected, string(body))
	status, headers, body = request(t, instance, http.MethodPost, url, `{"metadata":{}}`, auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	assert.JSONEq(t, expected, string(body))

	for _, invalid := range []string{
		`{}`,
		`{"metadata":null}`,
		`{"metadata":{"revise":"must-not-change","added":null}}`,
		`{"metadata":{"revise":"must-not-change","added":5}}`,
		`{"metadata":{"revise":"must-not-change","revise":"duplicate"}}`,
		`{"metadata":{"revise":"must-not-change"},"extra":true}`,
	} {
		status, headers, body := request(t, instance, http.MethodPost, url, invalid, auth)
		require.Equal(t, http.StatusBadRequest, status, "%s: %s", invalid, body)
		requireJSON(t, headers)
		var fault map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(body, &fault))
		assert.Contains(t, fault, "badRequest")
	}
	status, _, body = request(t, instance, http.MethodPost, url, patch, nil)
	require.Equal(t, http.StatusUnauthorized, status, "%s", body)
	const missing = "00000000-0000-0000-0000-000000000000"
	status, headers, body = request(t, instance, http.MethodPost, collection+"/"+missing+"/metadata", patch, auth)
	require.Equal(t, http.StatusNotFound, status, "%s", body)
	requireJSON(t, headers)
	status, headers, body = request(t, instance, http.MethodGet, url, "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	assert.JSONEq(t, expected, string(body))
}
