package server_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/volumes"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/servers"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/volumeattach"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const fixtureAttachmentPath = "/servers/" + fixtureServerID + "/os-volume_attachments"

func TestVolumeAttachmentWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	projectID := volumeProjectID(t, provider)
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	collection := computePath + "/" + projectID + fixtureAttachmentPath
	const payload = `{"volumeAttachment":{"volumeId":"` + fixtureVolumeID + `"}}`
	const missingID = "00000000-0000-0000-0000-000000000000"
	for _, invalid := range []struct {
		name   string
		path   string
		body   string
		auth   map[string]string
		status int
	}{
		{"missing token", collection, payload, nil, http.StatusUnauthorized},
		{"foreign URL project", computePath + "/" + missingID + fixtureAttachmentPath, payload, auth, http.StatusBadRequest},
		{"missing volume", collection, `{"volumeAttachment":{"volumeId":"` + missingID + `"}}`, auth, http.StatusNotFound},
		{
			name:   "device outside fixture scope",
			path:   collection,
			body:   `{"volumeAttachment":{"volumeId":"` + fixtureVolumeID + `","device":"/dev/sda"}}`,
			auth:   auth,
			status: http.StatusBadRequest,
		},
	} {
		t.Run(invalid.name, func(t *testing.T) {
			status, _, body := request(t, instance, http.MethodPost, invalid.path, invalid.body, invalid.auth)
			assert.Equal(t, invalid.status, status, "%s", body)
		})
	}

	// https://docs.openstack.org/api-ref/compute/#attach-a-volume-to-an-instance
	status, headers, body := request(t, instance, http.MethodPost, collection, payload, auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	var attached struct {
		Attachment wireNovaAttachment `json:"volumeAttachment"`
	}
	require.NoError(t, json.Unmarshal(body, &attached))
	want := wireNovaAttachment{
		ID: fixtureVolumeID, VolumeID: fixtureVolumeID, ServerID: fixtureServerID, Device: "/dev/vdb",
	}
	assert.Equal(t, want, attached.Attachment)

	status, _, body = request(t, instance, http.MethodGet, collection, "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	var listing struct {
		Attachments []wireNovaAttachment `json:"volumeAttachments"`
	}
	require.NoError(t, json.Unmarshal(body, &listing))
	assert.Equal(t, []wireNovaAttachment{want}, listing.Attachments)
	status, _, body = request(t, instance, http.MethodGet, computePath+fixtureAttachmentPath+"/"+fixtureVolumeID, "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	var detail struct {
		Attachment wireNovaAttachment `json:"volumeAttachment"`
	}
	require.NoError(t, json.Unmarshal(body, &detail))
	assert.Equal(t, want, detail.Attachment)

	// https://docs.openstack.org/api-ref/block-storage/v3/#show-a-volume-s-details
	volumeURL := volumePath + "/" + projectID + "/volumes/" + fixtureVolumeID
	status, headers, body = request(t, instance, http.MethodGet, volumeURL, "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	var cinder struct {
		Volume struct {
			Status      string                 `json:"status"`
			Attachments []wireCinderAttachment `json:"attachments"`
		} `json:"volume"`
	}
	require.NoError(t, json.Unmarshal(body, &cinder))
	assert.Equal(t, "in-use", cinder.Volume.Status)
	require.Len(t, cinder.Volume.Attachments, 1)
	observed := cinder.Volume.Attachments[0]
	assert.Equal(t, fixtureVolumeID, observed.ID)
	assert.Equal(t, fixtureVolumeID, observed.VolumeID)
	assert.Equal(t, fixtureServerID, observed.ServerID)
	assert.Equal(t, want.Device, observed.Device)
	assert.NotEmpty(t, observed.AttachmentID)
	assert.NotEqual(t, fixtureVolumeID, observed.AttachmentID)
	_, err := time.Parse("2006-01-02T15:04:05.000000", observed.AttachedAt)
	assert.NoError(t, err)

	// https://docs.openstack.org/api-ref/compute/#detach-a-volume-from-an-instance
	entry := collection + "/" + fixtureVolumeID
	status, _, body = request(t, instance, http.MethodDelete, entry, "", auth)
	require.Equal(t, http.StatusAccepted, status, "%s", body)
	assert.Empty(t, body)
	status, _, body = request(t, instance, http.MethodGet, entry, "", auth)
	assert.Equal(t, http.StatusNotFound, status, "%s", body)
	status, _, body = request(t, instance, http.MethodGet, collection, "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	assert.JSONEq(t, `{"volumeAttachments":[]}`, string(body))
}

func TestGophercloudAttachesAndDetachesVolumeAcrossServices(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	discover := func(provider *gophercloud.ProviderClient) (*gophercloud.ServiceClient, *gophercloud.ServiceClient) {
		t.Helper()
		endpoint := gophercloud.EndpointOpts{Region: "RegionOne", Availability: gophercloud.AvailabilityPublic}
		compute, err := openstack.NewComputeV2(provider, endpoint)
		require.NoError(t, err)
		storage, err := openstack.NewBlockStorageV3(provider, endpoint)
		require.NoError(t, err)
		return compute, storage
	}
	compute, storage := discover(provider)
	list := func(client *gophercloud.ServiceClient) []volumeattach.VolumeAttachment {
		t.Helper()
		page, err := volumeattach.List(client, fixtureServerID).AllPages(t.Context())
		require.NoError(t, err)
		result, err := volumeattach.ExtractVolumeAttachments(page)
		require.NoError(t, err)
		return result
	}
	read := func(client *gophercloud.ServiceClient, volumeID string) *volumes.Volume {
		t.Helper()
		result, err := volumes.Get(t.Context(), client, volumeID).Extract()
		require.NoError(t, err)
		require.NotNil(t, result)
		return result
	}
	attach := func(opts volumeattach.CreateOpts) *volumeattach.VolumeAttachment {
		t.Helper()
		result, err := volumeattach.Create(t.Context(), compute, fixtureServerID, opts).Extract()
		require.NoError(t, err)
		require.NotNil(t, result)
		return result
	}
	assert.Empty(t, list(compute))
	admin := map[string]string{"X-SandStack-Admin-Token": adminToken}
	created, err := volumes.Create(t.Context(), storage, volumes.CreateOpts{Size: 2, Name: "attach-volume"}, nil).Extract()
	require.NoError(t, err)
	require.NotNil(t, created)
	require.NotEmpty(t, created.ID)
	status, _, body := request(t, instance, http.MethodPut, volumeCompleteCreatePath(created.ID), "", admin)
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	require.Equal(t, "available", read(storage, created.ID).Status)

	opts := volumeattach.CreateOpts{VolumeID: created.ID, Device: "/dev/vdc"}
	stale := "/_sandstack/compute/servers/" + fixtureServerID + "/stale-detail"
	setControl := func(method, path string) {
		t.Helper()
		status, _, body := request(t, instance, method, path, "", admin)
		require.Equal(t, http.StatusNoContent, status, "%s", body)
	}
	setControl(http.MethodPut, stale)
	setControl(http.MethodPut, faultPath)
	staleServer, err := servers.Get(t.Context(), compute, fixtureServerID).Extract()
	require.NoError(t, err)
	require.Equal(t, "ACTIVE", staleServer.Status)
	_, err = volumeattach.Create(t.Context(), compute, fixtureServerID, opts).Extract()
	require.Error(t, err)
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusConflict), "%v", err)
	assert.Empty(t, list(compute))
	assert.Equal(t, "available", read(storage, created.ID).Status)
	setControl(http.MethodDelete, faultPath)
	setControl(http.MethodDelete, stale)
	setControl(http.MethodPut, unavailableControl)

	attached := attach(opts)
	assert.Equal(t, &volumeattach.VolumeAttachment{
		ID: created.ID, VolumeID: created.ID, ServerID: fixtureServerID, Device: opts.Device,
	}, attached)
	assert.Equal(t, []volumeattach.VolumeAttachment{*attached}, list(compute))
	observed, err := volumeattach.Get(t.Context(), compute, fixtureServerID, created.ID).Extract()
	require.NoError(t, err)
	assert.Equal(t, attached, observed)
	_, err = servers.Get(t.Context(), compute, fixtureServerID).Extract()
	require.Error(t, err)
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusServiceUnavailable), "%v", err)
	inUse := read(storage, created.ID)
	assert.Equal(t, "in-use", inUse.Status)
	assert.Equal(t, created.Size, inUse.Size)
	assert.Equal(t, created.CreatedAt, inUse.CreatedAt)
	require.Len(t, inUse.Attachments, 1)
	link := inUse.Attachments[0]
	assert.Equal(t, created.ID, link.ID)
	assert.Equal(t, created.ID, link.VolumeID)
	assert.Equal(t, fixtureServerID, link.ServerID)
	assert.Equal(t, opts.Device, link.Device)
	assert.NotEmpty(t, link.AttachmentID)
	assert.NotEqual(t, created.ID, link.AttachmentID)
	assert.False(t, link.AttachedAt.IsZero())

	_, err = volumeattach.Create(t.Context(), compute, fixtureServerID, opts).Extract()
	require.Error(t, err)
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusConflict), "%v", err)
	err = volumes.Delete(t.Context(), storage, created.ID, nil).ExtractErr()
	require.Error(t, err)
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusBadRequest), "%v", err)
	faultVolume := "/_sandstack/volume/volumes/" + created.ID + "/fault"
	status, _, body = request(t, instance, http.MethodPut, faultVolume, "", admin)
	assert.Equal(t, http.StatusConflict, status, "%s", body)
	assert.Equal(t, []volumeattach.VolumeAttachment{*attached}, list(compute))
	assert.Equal(t, inUse, read(storage, created.ID))

	require.NoError(t, volumeattach.Delete(t.Context(), compute, fixtureServerID, created.ID).ExtractErr())
	assert.Empty(t, list(compute))
	detached := read(storage, created.ID)
	assert.Equal(t, "available", detached.Status)
	assert.Equal(t, []volumes.Attachment{}, detached.Attachments)
	require.NoError(t, volumes.Delete(t.Context(), storage, created.ID, nil).ExtractErr())
	_, err = volumes.Get(t.Context(), storage, created.ID).Extract()
	require.Error(t, err)
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusNotFound), "%v", err)

	attach(volumeattach.CreateOpts{VolumeID: fixtureVolumeID})
	require.Equal(t, "in-use", read(storage, fixtureVolumeID).Status)
	status, _, body = request(t, instance, http.MethodPost, "/_sandstack/reset", "", admin)
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	_, err = volumeattach.Get(t.Context(), compute, fixtureServerID, fixtureVolumeID).Extract()
	require.Error(t, err)
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusUnauthorized), "%v", err)
	newCompute, newStorage := discover(authenticate(t, instance))
	assert.Empty(t, list(newCompute))
	reset := read(newStorage, fixtureVolumeID)
	assert.Equal(t, "available", reset.Status)
	assert.Equal(t, []volumes.Attachment{}, reset.Attachments)
}

type wireNovaAttachment struct {
	ID       string `json:"id"`
	VolumeID string `json:"volumeId"`
	ServerID string `json:"serverId"`
	Device   string `json:"device"`
}

type wireCinderAttachment struct {
	ID           string `json:"id"`
	AttachmentID string `json:"attachment_id"`
	VolumeID     string `json:"volume_id"`
	ServerID     string `json:"server_id"`
	Device       string `json:"device"`
	AttachedAt   string `json:"attached_at"`
}
