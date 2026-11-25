package server_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/volumes"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/volumeattach"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGophercloudSetsBootableOnAttachedImageBackedVolume(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	endpoint := gophercloud.EndpointOpts{Region: "RegionOne", Availability: gophercloud.AvailabilityPublic}
	storage, err := openstack.NewBlockStorageV3(provider, endpoint)
	require.NoError(t, err)
	compute, err := openstack.NewComputeV2(provider, endpoint)
	require.NoError(t, err)
	ctx := t.Context()
	admin := map[string]string{"X-SandStack-Admin-Token": adminToken}
	created, err := volumes.Create(ctx, storage, volumes.CreateOpts{
		Name: "attached bootable volume", Size: 1, ImageID: fixtureImageID,
		Metadata: map[string]string{"owner": "fixture team"},
	}, nil).Extract()
	require.NoError(t, err)
	require.NotEmpty(t, created.ID)
	status, _, body := request(t, instance, http.MethodPut, volumeCompleteCreatePath(created.ID), "", admin)
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	attachment, err := volumeattach.Create(ctx, compute, fixtureServerID, volumeattach.CreateOpts{
		VolumeID: created.ID,
	}).Extract()
	require.NoError(t, err)
	read := func() *volumes.Volume {
		t.Helper()
		volume, err := volumes.Get(ctx, storage, created.ID).Extract()
		require.NoError(t, err)
		return volume
	}
	baseline := read()
	require.Equal(t, "true", baseline.Bootable)
	require.Equal(t, "in-use", baseline.Status)
	require.Equal(t, fixtureImageID, baseline.VolumeImageMetadata["image_id"])
	require.Len(t, baseline.Attachments, 1)
	status, _, body = request(t, instance, http.MethodPut,
		"/_sandstack/volume/volumes/"+created.ID+"/unavailable-detail", "", admin)
	require.Equal(t, http.StatusNoContent, status, "%s", body)

	err = volumes.SetBootable(ctx, storage, created.ID, volumes.BootableOpts{Bootable: false}).ExtractErr()
	require.NoError(t, err)
	_, err = volumes.Get(ctx, storage, created.ID).Extract()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusServiceUnavailable), "detail outage remains pending: %v", err)

	disabled := read()
	assert.True(t, disabled.UpdatedAt.After(baseline.UpdatedAt))
	expected := *baseline
	expected.Bootable = "false"
	expected.UpdatedAt = disabled.UpdatedAt
	assert.Equal(t, &expected, disabled)

	err = volumes.SetBootable(ctx, storage, created.ID, volumes.BootableOpts{Bootable: true}).ExtractErr()
	require.NoError(t, err)

	restored := read()
	assert.True(t, restored.UpdatedAt.After(disabled.UpdatedAt))
	expected = *baseline
	expected.UpdatedAt = restored.UpdatedAt
	assert.Equal(t, &expected, restored)
	stillAttached, err := volumeattach.Get(ctx, compute, fixtureServerID, created.ID).Extract()
	require.NoError(t, err)
	assert.Equal(t, attachment, stillAttached)
}

func TestCinderSetBootableWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	collection := volumePath + "/" + volumeProjectID(t, provider) + "/volumes"
	auth := map[string]string{"X-Auth-Token": provider.Token(), "OpenStack-API-Version": "volume 3.0"}
	status, _, body := request(t, instance, http.MethodPost, collection, `{"volume":{"size":1}}`, auth)
	require.Equal(t, http.StatusAccepted, status, "%s", body)
	var created struct {
		Volume wireVolume `json:"volume"`
	}
	require.NoError(t, json.Unmarshal(body, &created))
	require.NotEmpty(t, created.Volume.ID)
	detail := collection + "/" + created.Volume.ID
	action := detail + "/action"
	check := func(wantBootable, wantState string) {
		t.Helper()
		status, headers, body := request(t, instance, http.MethodGet, detail, "", auth)
		require.Equal(t, http.StatusOK, status, "%s", body)
		requireJSON(t, headers)
		var result struct {
			Volume struct {
				Bootable string `json:"bootable"`
				Status   string `json:"status"`
			} `json:"volume"`
		}
		require.NoError(t, json.Unmarshal(body, &result))
		assert.Equal(t, wantBootable, result.Volume.Bootable)
		assert.Equal(t, wantState, result.Volume.Status)
	}

	// https://docs.openstack.org/api-ref/block-storage/v3/#update-a-volume-s-bootable-status
	// https://github.com/openstack/cinder/blob/master/cinder/api/validation/parameter_types.py#L126-L131
	for _, item := range []struct{ value, want string }{
		{"true", "true"}, {"false", "false"}, {`"YES"`, "true"}, {`"Off"`, "false"},
		{`"1"`, "true"}, {`"0"`, "false"}, {`"t"`, "true"},
	} {
		payload := fmt.Sprintf(`{"os-set_bootable":{"bootable":%s}}`, item.value)
		status, headers, body := request(t, instance, http.MethodPost, action, payload, auth)
		assert.Equal(t, http.StatusOK, status, "%s: %s", payload, body)
		assert.Equal(t, "volume 3.0", headers.Get("OpenStack-API-Version"))
		assert.Empty(t, body)
		check(item.want, "creating")
	}
	for _, invalid := range []string{
		`{"os-set_bootable":{}}`,
		`{"os-set_bootable":{"bootable":null}}`,
		`{"os-set_bootable":{"bootable":0}}`,
		`{"os-set_bootable":{"bootable":"tRuE"}}`,
		`{"os-set_bootable":{"bootable":"yes "}}`,
		`{"os-set_bootable":{"bootable":false,"extra":true}}`,
		`{"os-set_bootable":{"bootable":true,"bootable":false}}`,
		`{"os-set_bootable":{"bootable":false},"extra":true}`,
	} {
		status, headers, body := request(t, instance, http.MethodPost, action, invalid, auth)
		assert.Equal(t, http.StatusBadRequest, status, "%s: %s", invalid, body)
		requireJSON(t, headers)
	}
	status, _, body = request(t, instance, http.MethodPost, collection+"/00000000-0000-0000-0000-000000000000/action",
		`{"os-set_bootable":{"bootable":true}}`, auth)
	assert.Equal(t, http.StatusNotFound, status, "%s", body)
	check("true", "creating")

	status, _, body = request(t, instance, http.MethodPut, volumeCompleteCreatePath(created.Volume.ID), "", map[string]string{
		"X-SandStack-Admin-Token": adminToken,
	})
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	check("true", "available")
}
