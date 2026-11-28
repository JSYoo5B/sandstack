package server_test

import (
	"encoding/json"
	"net/http"
	"regexp"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/instanceactions"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/servers"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/volumeattach"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var novaRequestIDPattern = regexp.MustCompile(`^req-[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestGophercloudFindsNovaServerActionsByRequestID(t *testing.T) {
	instance := startServer(t, "")
	newCompute := func() *gophercloud.ServiceClient {
		t.Helper()
		client, err := openstack.NewComputeV2(authenticate(t, instance), gophercloud.EndpointOpts{
			Region: "RegionOne", Availability: gophercloud.AvailabilityPublic,
		})
		require.NoError(t, err)
		return client
	}
	compute := newCompute()
	ctx := t.Context()
	list := func() []instanceactions.InstanceAction {
		t.Helper()
		pages, err := instanceactions.List(compute, fixtureServerID, nil).AllPages(ctx)
		require.NoError(t, err)
		actions, err := instanceactions.ExtractInstanceActions(pages)
		require.NoError(t, err)
		return actions
	}
	server, err := servers.Get(ctx, compute, fixtureServerID).Extract()
	require.NoError(t, err)
	initial := list()
	require.Len(t, initial, 1)
	created := initial[0]

	stop := servers.Stop(ctx, compute, fixtureServerID)
	require.NoError(t, stop.ExtractErr())
	stopID := stop.Header.Get("X-Openstack-Request-Id")
	attach := volumeattach.Create(ctx, compute, fixtureServerID, volumeattach.CreateOpts{VolumeID: fixtureVolumeID})
	_, err = attach.Extract()
	require.NoError(t, err)
	paused := servers.Pause(ctx, compute, fixtureServerID).ExtractErr()
	lock := servers.Lock(ctx, compute, fixtureServerID)
	require.NoError(t, lock.ExtractErr())
	repeatedLock := servers.Lock(ctx, compute, fixtureServerID)
	require.NoError(t, repeatedLock.ExtractErr())
	actions := list()
	detail, err := instanceactions.Get(ctx, compute, fixtureServerID, stopID).Extract()
	require.NoError(t, err)

	assert.Equal(t, "create", created.Action)
	assert.Equal(t, fixtureServerID, created.InstanceUUID)
	assert.Equal(t, server.TenantID, created.ProjectID)
	assert.Equal(t, server.UserID, created.UserID)
	assert.True(t, created.StartTime.Equal(server.Created), "%v != %v", created.StartTime, server.Created)
	assert.Regexp(t, novaRequestIDPattern, created.RequestID)
	assert.True(t, gophercloud.ResponseCodeIs(paused, http.StatusConflict), "%v", paused)
	assert.Regexp(t, novaRequestIDPattern, stopID)
	assert.Equal(t, stopID, stop.Header.Get("X-Compute-Request-Id"))
	names := make([]string, len(actions))
	requestIDs := make([]string, len(actions))
	for i, action := range actions {
		names[i] = action.Action
		requestIDs[i] = action.RequestID
	}
	assert.Equal(t, []string{"lock", "attach_volume", "stop", "create"}, names)
	assert.Equal(t, []string{
		lock.Header.Get("X-Openstack-Request-Id"), attach.Header.Get("X-Openstack-Request-Id"),
		stopID, created.RequestID,
	}, requestIDs)
	assert.NotContains(t, requestIDs, repeatedLock.Header.Get("X-Openstack-Request-Id"))
	assert.Equal(t, "stop", detail.Action)
	assert.Equal(t, stopID, detail.RequestID)
	assert.Equal(t, fixtureServerID, detail.InstanceUUID)
	assert.Equal(t, server.TenantID, detail.ProjectID)
	assert.Equal(t, server.UserID, detail.UserID)
	assert.Empty(t, detail.Message)
	assert.Nil(t, detail.Events)
	assert.False(t, detail.StartTime.Before(created.StartTime))

	_, err = instanceactions.Get(ctx, compute, fixtureServerID, "req-00000000-0000-4000-8000-000000000000").Extract()

	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusNotFound), "%v", err)

	status, _, body := request(t, instance, http.MethodPost, "/_sandstack/reset", "",
		map[string]string{"X-SandStack-Admin-Token": adminToken})
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	compute = newCompute()

	assert.Equal(t, initial, list())
	_, err = instanceactions.Get(ctx, compute, fixtureServerID, stopID).Extract()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusNotFound), "%v", err)
}

func TestNovaInstanceActionsWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	projectPath := computePath + "/" + routerProjectID(t, provider)
	path := projectPath + "/servers/" + fixtureServerID
	actions := path + "/os-instance-actions"
	auth := map[string]string{"X-Auth-Token": provider.Token(), "OpenStack-API-Version": "compute 2.1"}
	startTime := regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{6}$`)

	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/compute_req_id.py#L32-L36
	status, headers, body := request(t, instance, http.MethodPost, path+"/action", `{"pause":null}`, auth)

	require.Equal(t, http.StatusAccepted, status, "%s", body)
	pauseID := headers.Get("X-Openstack-Request-Id")
	assert.Regexp(t, novaRequestIDPattern, pauseID)
	assert.Equal(t, pauseID, headers.Get("X-Compute-Request-Id"))

	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L4852-L4858
	status, headers, body = request(t, instance, http.MethodPost, path+"/action", `{"pause":null}`, auth)

	requireNovaFault(t, http.StatusConflict, "conflictingRequest", status, headers, body)
	conflictID := headers.Get("X-Openstack-Request-Id")
	assert.Regexp(t, novaRequestIDPattern, conflictID)
	assert.NotEqual(t, pauseID, conflictID)

	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/schemas/instance_actions.py#L55-L101
	status, headers, body = request(t, instance, http.MethodGet, actions+"?limit=1", "", auth)

	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	var listed struct {
		Actions []map[string]json.RawMessage `json:"instanceActions"`
	}
	require.NoError(t, json.Unmarshal(body, &listed))
	require.Len(t, listed.Actions, 2)
	latest := listed.Actions[0]
	assert.Len(t, latest, 7)
	assert.JSONEq(t, `"pause"`, string(latest["action"]))
	assert.JSONEq(t, `"`+fixtureServerID+`"`, string(latest["instance_uuid"]))
	assert.JSONEq(t, `"`+pauseID+`"`, string(latest["request_id"]))
	assert.JSONEq(t, `null`, string(latest["message"]))
	assert.Contains(t, latest, "project_id")
	assert.Contains(t, latest, "user_id")
	var started string
	require.NoError(t, json.Unmarshal(latest["start_time"], &started))
	assert.Regexp(t, startTime, started)
	assert.JSONEq(t, `"create"`, string(listed.Actions[1]["action"]))

	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/instance_actions.py#L167-L186
	status, headers, body = request(t, instance, http.MethodGet, actions+"/"+pauseID, "", auth)

	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	var shown struct {
		Action map[string]json.RawMessage `json:"instanceAction"`
	}
	require.NoError(t, json.Unmarshal(body, &shown))
	assert.Equal(t, latest, shown.Action)
	assert.NotContains(t, shown.Action, "events")

	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/instance_actions.py#L155-L159
	for _, missing := range []string{conflictID, "unknown"} {
		status, headers, body = request(t, instance, http.MethodGet, actions+"/"+missing, "", auth)

		requireNovaFault(t, http.StatusNotFound, "itemNotFound", status, headers, body)
	}
	const otherServer = "/servers/00000000-0000-0000-0000-000000000000/os-instance-actions"
	status, headers, body = request(t, instance, http.MethodGet, projectPath+otherServer, "", auth)

	requireNovaFault(t, http.StatusNotFound, "itemNotFound", status, headers, body)
}
