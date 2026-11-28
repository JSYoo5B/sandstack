package server_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/transfers"
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/volumes"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/volumeattach"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/api/contrib/volume_transfer.py#L42-L145
// https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/transfer/api.py#L131-L197
// https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/transfer/api.py#L235-L351
func TestGophercloudTransfersVolume(t *testing.T) {
	instance := startServer(t, "")
	endpoint := gophercloud.EndpointOpts{Region: "RegionOne", Availability: gophercloud.AvailabilityPublic}
	donor, err := openstack.NewBlockStorageV3(authenticate(t, instance), endpoint)
	require.NoError(t, err)
	receiver, err := openstack.NewBlockStorageV3(authenticate(t, instance), endpoint)
	require.NoError(t, err)
	compute, err := openstack.NewComputeV2(authenticate(t, instance), endpoint)
	require.NoError(t, err)
	ctx := t.Context()
	readVolume := func() *volumes.Volume {
		t.Helper()
		volume, err := volumes.Get(ctx, donor, fixtureVolumeID).Extract()
		require.NoError(t, err)
		return volume
	}
	listTransfers := func() []transfers.Transfer {
		t.Helper()
		pages, err := transfers.List(donor, nil).AllPages(ctx)
		require.NoError(t, err)
		listed, err := transfers.ExtractTransfers(pages)
		require.NoError(t, err)
		return listed
	}

	created, err := transfers.Create(ctx, donor, transfers.CreateOpts{VolumeID: fixtureVolumeID, Name: "handoff"}).Extract()
	require.NoError(t, err)
	read, err := transfers.Get(ctx, donor, created.ID).Extract()
	require.NoError(t, err)
	listed := listTransfers()
	held := readVolume()
	_, repeatErr := transfers.Create(ctx, donor, transfers.CreateOpts{VolumeID: fixtureVolumeID}).Extract()
	deleteErr := volumes.Delete(ctx, donor, fixtureVolumeID, nil).ExtractErr()
	_, attachErr := volumeattach.Create(ctx, compute, fixtureServerID, volumeattach.CreateOpts{
		VolumeID: fixtureVolumeID,
	}).Extract()

	assert.Len(t, created.AuthKey, 16)
	assert.Equal(t, "handoff", created.Name)
	assert.Equal(t, fixtureVolumeID, created.VolumeID)
	assert.False(t, created.CreatedAt.IsZero())
	assert.Empty(t, read.AuthKey)
	assert.Equal(t, created.CreatedAt, read.CreatedAt)
	assert.Equal(t, created.Links, read.Links)
	require.Len(t, listed, 1)
	assert.Equal(t, *read, listed[0])
	assert.Equal(t, "awaiting-transfer", held.Status)
	assert.True(t, gophercloud.ResponseCodeIs(repeatErr, http.StatusBadRequest), "%v", repeatErr)
	assert.True(t, gophercloud.ResponseCodeIs(deleteErr, http.StatusBadRequest), "%v", deleteErr)
	assert.True(t, gophercloud.ResponseCodeIs(attachErr, http.StatusConflict), "%v", attachErr)

	_, wrongKeyErr := transfers.Accept(ctx, receiver, created.ID, transfers.AcceptOpts{AuthKey: "wrong"}).Extract()
	accepted, err := transfers.Accept(ctx, receiver, created.ID, transfers.AcceptOpts{AuthKey: created.AuthKey}).Extract()
	require.NoError(t, err)
	owned := readVolume()
	_, readAcceptedErr := transfers.Get(ctx, donor, created.ID).Extract()

	assert.True(t, gophercloud.ResponseCodeIs(wrongKeyErr, http.StatusBadRequest), "%v", wrongKeyErr)
	assert.Equal(t, created.ID, accepted.ID)
	assert.Equal(t, fixtureVolumeID, accepted.VolumeID)
	assert.Equal(t, "handoff", accepted.Name)
	assert.Empty(t, accepted.AuthKey)
	assert.Equal(t, "available", owned.Status)
	assert.Equal(t, held.TenantID, owned.TenantID)
	assert.True(t, gophercloud.ResponseCodeIs(readAcceptedErr, http.StatusNotFound), "%v", readAcceptedErr)
	assert.Empty(t, listTransfers())

	cancelled, err := transfers.Create(ctx, donor, transfers.CreateOpts{VolumeID: fixtureVolumeID}).Extract()
	require.NoError(t, err)
	require.NoError(t, transfers.Delete(ctx, donor, cancelled.ID).ExtractErr())
	_, acceptCancelledErr := transfers.Accept(ctx, receiver, cancelled.ID, transfers.AcceptOpts{
		AuthKey: cancelled.AuthKey,
	}).Extract()

	assert.Empty(t, cancelled.Name)
	assert.Equal(t, "available", readVolume().Status)
	assert.True(t, gophercloud.ResponseCodeIs(acceptCancelledErr, http.StatusNotFound), "%v", acceptCancelledErr)
	assert.Empty(t, listTransfers())
}

type wireFault map[string]struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func requireFault(t *testing.T, body []byte, status int, kind, message string) {
	t.Helper()
	var fault wireFault
	require.NoError(t, json.Unmarshal(body, &fault))
	require.Contains(t, fault, kind)
	assert.Equal(t, status, fault[kind].Code)
	assert.Equal(t, message, fault[kind].Message)
}

// https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/api/contrib/volume_transfer.py#L42-L164
// https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/api/schemas/volume_transfer.py#L23-L58
// https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/api/validation/validators.py#L150-L163
// https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/api/views/transfers.py#L39-L98
// https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/transfer/api.py#L235-L245
// https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/exception.py#L219-L244
// https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/exception.py#L335-L336
// https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/exception.py#L791-L792
func TestCinderVolumeTransferWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	projectID := volumeProjectID(t, provider)
	project := volumePath + "/" + projectID
	collection := project + "/os-volume-transfer"
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	receiver := map[string]string{"X-Auth-Token": authenticate(t, instance).Token()}
	admin := map[string]string{"X-SandStack-Admin-Token": adminToken}
	readVolumeStatus := func() string {
		t.Helper()
		status, _, body := request(t, instance, http.MethodGet, project+"/volumes/"+fixtureVolumeID, "", auth)
		require.Equal(t, http.StatusOK, status, "%s", body)
		var response struct {
			Volume wireVolume `json:"volume"`
		}
		require.NoError(t, json.Unmarshal(body, &response))
		return response.Volume.Status
	}

	for _, path := range []string{collection, collection + "/detail"} {
		status, headers, body := request(t, instance, http.MethodGet, path, "", auth)
		require.Equal(t, http.StatusOK, status, "%s", body)
		requireJSON(t, headers)
		assert.JSONEq(t, `{"transfers":[]}`, string(body))
	}
	status, _, body := request(t, instance, http.MethodPost, collection,
		`{"transfer":{"volume_id":"`+fixtureVolumeID+`"}}`, nil)
	assert.Equal(t, http.StatusUnauthorized, status, "%s", body)
	for _, tc := range []struct {
		name string
		body string
	}{
		{"missing envelope", `{}`},
		{"null transfer", `{"transfer":null}`},
		{"extra envelope", `{"transfer":{"volume_id":"` + fixtureVolumeID + `"},"extra":{}}`},
		{"missing volume", `{"transfer":{"name":"handoff"}}`},
		{"invalid volume", `{"transfer":{"volume_id":"volume"}}`},
		{"null volume", `{"transfer":{"volume_id":null}}`},
		{"blank name", `{"transfer":{"volume_id":"` + fixtureVolumeID + `","name":"  "}}`},
		{"numeric name", `{"transfer":{"volume_id":"` + fixtureVolumeID + `","name":1}}`},
		{"no_snapshots before 3.55", `{"transfer":{"volume_id":"` + fixtureVolumeID + `","no_snapshots":true}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, headers, body := request(t, instance, http.MethodPost, collection, tc.body, auth)

			assert.Equal(t, http.StatusBadRequest, status, "%s", body)
			requireJSON(t, headers)
		})
	}
	status, _, body = request(t, instance, http.MethodPost, collection,
		`{"transfer":{"volume_id":"`+missingVolumeMarker+`"}}`, auth)
	assert.Equal(t, http.StatusNotFound, status, "%s", body)
	requireFault(t, body, http.StatusNotFound, "itemNotFound", "Volume "+missingVolumeMarker+" could not be found.")
	assert.Equal(t, "available", readVolumeStatus())

	status, headers, body := request(t, instance, http.MethodPost, collection,
		`{"transfer":{"volume_id":"`+fixtureVolumeID+`","name":"  wire  "}}`, auth)
	require.Equal(t, http.StatusAccepted, status, "%s", body)
	requireJSON(t, headers)
	var created struct {
		Transfer struct {
			ID        string       `json:"id"`
			CreatedAt string       `json:"created_at"`
			Name      *string      `json:"name"`
			VolumeID  string       `json:"volume_id"`
			AuthKey   string       `json:"auth_key"`
			Links     []volumeLink `json:"links"`
		} `json:"transfer"`
	}
	require.NoError(t, json.Unmarshal(body, &created))
	var createdFields map[string]map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(body, &createdFields))
	transferID := created.Transfer.ID
	resource := "/" + projectID + "/os-volume-transfer/" + transferID

	assert.Len(t, createdFields["transfer"], 6)
	require.NotNil(t, created.Transfer.Name)
	assert.Equal(t, "wire", *created.Transfer.Name)
	assert.Equal(t, fixtureVolumeID, created.Transfer.VolumeID)
	assert.Regexp(t, `^[0-9a-f]{16}$`, created.Transfer.AuthKey)
	assert.Regexp(t, `^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{6}$`, created.Transfer.CreatedAt)
	assert.Equal(t, []volumeLink{
		{Rel: "self", Href: instance.URL + volumePath + resource},
		{Rel: "bookmark", Href: instance.URL + "/volume" + resource},
	}, created.Transfer.Links)
	assert.Equal(t, "awaiting-transfer", readVolumeStatus())

	status, _, body = request(t, instance, http.MethodPost, collection,
		`{"transfer":{"volume_id":"`+fixtureVolumeID+`"}}`, auth)
	assert.Equal(t, http.StatusBadRequest, status, "%s", body)
	requireFault(t, body, http.StatusBadRequest, "badRequest", "Invalid volume: status must be available")
	status, _, body = request(t, instance, http.MethodDelete, project+"/volumes/"+fixtureVolumeID, "", auth)
	assert.Equal(t, http.StatusBadRequest, status, "%s", body)
	status, _, body = request(t, instance, http.MethodPut,
		"/_sandstack/volume/volumes/"+fixtureVolumeID+"/fault", "", admin)
	assert.Equal(t, http.StatusConflict, status, "%s", body)
	assert.Equal(t, "awaiting-transfer", readVolumeStatus())

	status, _, body = request(t, instance, http.MethodGet, collection+"/"+transferID, "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	var shown map[string]map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(body, &shown))
	status, _, body = request(t, instance, http.MethodGet, collection+"/detail", "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	var detailed map[string][]map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(body, &detailed))
	status, _, body = request(t, instance, http.MethodGet, collection, "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	var summarized map[string][]map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(body, &summarized))

	for _, field := range []string{"id", "created_at", "name", "volume_id", "links"} {
		assert.Equal(t, createdFields["transfer"][field], shown["transfer"][field], field)
	}
	assert.Len(t, shown["transfer"], 5)
	assert.Equal(t, []map[string]json.RawMessage{shown["transfer"]}, detailed["transfers"])
	require.Len(t, summarized["transfers"], 1)
	assert.Len(t, summarized["transfers"][0], 4)
	for _, field := range []string{"id", "name", "volume_id", "links"} {
		assert.Equal(t, createdFields["transfer"][field], summarized["transfers"][0][field], field)
	}
	for _, path := range []string{collection + "?limit=1", collection + "/detail?all_tenants=1"} {
		status, _, body = request(t, instance, http.MethodGet, path, "", auth)
		assert.Equal(t, http.StatusBadRequest, status, "%s", body)
	}
	status, _, body = request(t, instance, http.MethodGet, collection+"/"+missingVolumeMarker, "", auth)
	assert.Equal(t, http.StatusNotFound, status, "%s", body)
	requireFault(t, body, http.StatusNotFound, "itemNotFound", "Transfer "+missingVolumeMarker+" could not be found.")

	acceptPath := collection + "/" + transferID + "/accept"
	for _, tc := range []struct {
		name string
		body string
	}{
		{"missing envelope", `{}`},
		{"missing key", `{"accept":{}}`},
		{"null key", `{"accept":{"auth_key":null}}`},
		{"boolean key", `{"accept":{"auth_key":true}}`},
		{"fractional key", `{"accept":{"auth_key":1.5}}`},
		{"extra field", `{"accept":{"auth_key":"` + created.Transfer.AuthKey + `","extra":1}}`},
	} {
		t.Run("accept "+tc.name, func(t *testing.T) {
			status, headers, body := request(t, instance, http.MethodPost, acceptPath, tc.body, receiver)

			assert.Equal(t, http.StatusBadRequest, status, "%s", body)
			requireJSON(t, headers)
		})
	}
	invalidKey := "Invalid auth key: Attempt to transfer " + transferID + " with invalid auth key."
	for _, key := range []string{`"0000000000000000"`, `12`} {
		status, _, body = request(t, instance, http.MethodPost, acceptPath, `{"accept":{"auth_key":`+key+`}}`, receiver)
		assert.Equal(t, http.StatusBadRequest, status, "%s", body)
		requireFault(t, body, http.StatusBadRequest, "badRequest", invalidKey)
	}
	status, _, body = request(t, instance, http.MethodPost, collection+"/"+missingVolumeMarker+"/accept",
		`{"accept":{"auth_key":"`+created.Transfer.AuthKey+`"}}`, receiver)
	assert.Equal(t, http.StatusNotFound, status, "%s", body)
	requireFault(t, body, http.StatusNotFound, "itemNotFound", "Transfer "+missingVolumeMarker+" could not be found.")
	assert.Equal(t, "awaiting-transfer", readVolumeStatus())

	status, headers, body = request(t, instance, http.MethodPost, acceptPath,
		`{"accept":{"auth_key":"`+created.Transfer.AuthKey+`"}}`, receiver)
	require.Equal(t, http.StatusAccepted, status, "%s", body)
	requireJSON(t, headers)
	var accepted map[string]map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(body, &accepted))

	assert.Equal(t, summarized["transfers"][0], accepted["transfer"])
	assert.Equal(t, "available", readVolumeStatus())
	status, _, body = request(t, instance, http.MethodGet, collection+"/"+transferID, "", auth)
	assert.Equal(t, http.StatusNotFound, status, "%s", body)
	status, _, body = request(t, instance, http.MethodPost, acceptPath,
		`{"accept":{"auth_key":"`+created.Transfer.AuthKey+`"}}`, receiver)
	assert.Equal(t, http.StatusNotFound, status, "%s", body)

	status, _, body = request(t, instance, http.MethodPost, collection,
		`{"transfer":{"volume_id":"`+fixtureVolumeID+`","name":null}}`, auth)
	require.Equal(t, http.StatusAccepted, status, "%s", body)
	var cancelled struct {
		Transfer struct {
			ID   string  `json:"id"`
			Name *string `json:"name"`
		} `json:"transfer"`
	}
	require.NoError(t, json.Unmarshal(body, &cancelled))
	status, _, body = request(t, instance, http.MethodDelete, collection+"/"+cancelled.Transfer.ID, "{}", auth)
	assert.Equal(t, http.StatusBadRequest, status, "%s", body)
	status, _, body = request(t, instance, http.MethodDelete, collection+"/"+missingVolumeMarker, "", auth)
	assert.Equal(t, http.StatusNotFound, status, "%s", body)
	requireFault(t, body, http.StatusNotFound, "itemNotFound", "Transfer "+missingVolumeMarker+" could not be found.")

	status, headers, body = request(t, instance, http.MethodDelete, collection+"/"+cancelled.Transfer.ID, "", auth)
	require.Equal(t, http.StatusAccepted, status, "%s", body)

	assert.Nil(t, cancelled.Transfer.Name)
	assert.Empty(t, body)
	assert.Equal(t, "no-store", headers.Get("Cache-Control"))
	assert.Equal(t, "available", readVolumeStatus())

	status, _, body = request(t, instance, http.MethodPost, collection,
		`{"transfer":{"volume_id":"`+fixtureVolumeID+`"}}`, auth)
	require.Equal(t, http.StatusAccepted, status, "%s", body)
	status, _, body = request(t, instance, http.MethodPost, "/_sandstack/reset", "", admin)
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	auth["X-Auth-Token"] = authenticate(t, instance).Token()
	status, _, body = request(t, instance, http.MethodGet, collection+"/detail", "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)

	assert.JSONEq(t, `{"transfers":[]}`, string(body))
	assert.Equal(t, "available", readVolumeStatus())
}
