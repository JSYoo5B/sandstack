package server_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/servergroups"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/servers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const noValidHostMessage = "No valid host was found. There are not enough hosts available."

func bootGroupMember(t *testing.T, client *gophercloud.ServiceClient, name, groupID string) string {
	t.Helper()
	created, err := servers.Create(t.Context(), client, servers.CreateOpts{
		Name: name, ImageRef: fixtureImageID, FlavorRef: "1",
	}, servers.SchedulerHintOpts{Group: groupID}).Extract()
	require.NoError(t, err)
	return created.ID
}

func listServerGroups(t *testing.T, client *gophercloud.ServiceClient) []servergroups.ServerGroup {
	t.Helper()
	pages, err := servergroups.List(client, servergroups.ListOpts{AllProjects: true}).AllPages(t.Context())
	require.NoError(t, err)
	found, err := servergroups.ExtractServerGroups(pages)
	require.NoError(t, err)
	return found
}

// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/server_groups.py#L91-L283
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L1577-L1626
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/scheduler/filters/affinity_filter.py#L82-L135
// https://github.com/gophercloud/gophercloud/blob/v2.15.0/openstack/compute/v2/servergroups/requests.go
func TestGophercloudManagesServerGroupsAndMembers(t *testing.T) {
	instance := startServer(t, "")
	clients := newBootClients(t, instance)
	ctx := t.Context()
	prepareBootNetwork(t, clients.network)
	group, err := servergroups.Create(ctx, clients.compute, servergroups.CreateOpts{
		Name: "web", Policies: []string{"anti-affinity"},
	}).Extract()
	require.NoError(t, err)

	first := bootGroupMember(t, clients.compute, "first", group.ID)
	second := bootGroupMember(t, clients.compute, "second", group.ID)

	assert.Equal(t, "web", group.Name)
	assert.Equal(t, []string{"anti-affinity"}, group.Policies)
	assert.Empty(t, group.Members)
	assert.Empty(t, group.Metadata)
	waitForServer(t, clients.compute, first, "ACTIVE")
	failed, err := servers.Get(ctx, clients.compute, second).Extract()
	require.NoError(t, err)
	assert.Equal(t, "ERROR", failed.Status)
	assert.Equal(t, 500, failed.Fault.Code)
	assert.Equal(t, noValidHostMessage, failed.Fault.Message)
	assert.Empty(t, serverPorts(t, clients.network, second))
	shown, err := servergroups.Get(ctx, clients.compute, group.ID).Extract()
	require.NoError(t, err)
	assert.Equal(t, []string{first, second}, shown.Members)
	listed := listServerGroups(t, clients.compute)
	require.Len(t, listed, 1)
	assert.Equal(t, *shown, listed[0])

	require.NoError(t, servers.Delete(ctx, clients.compute, first).ExtractErr())
	require.NoError(t, servers.Delete(ctx, clients.compute, second).ExtractErr())
	third := bootGroupMember(t, clients.compute, "third", group.ID)

	waitForServer(t, clients.compute, third, "ACTIVE")
	shown, err = servergroups.Get(ctx, clients.compute, group.ID).Extract()
	require.NoError(t, err)
	assert.Equal(t, []string{third}, shown.Members)

	require.NoError(t, servergroups.Delete(ctx, clients.compute, group.ID).ExtractErr())

	_, err = servergroups.Get(ctx, clients.compute, group.ID).Extract()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusNotFound), "%v", err)
	assert.Empty(t, listServerGroups(t, clients.compute))
	_, err = servers.Get(ctx, clients.compute, third).Extract()
	assert.NoError(t, err)
}

func TestNovaServerGroupWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	clients := newBootClients(t, instance)
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	admin := map[string]string{"X-SandStack-Admin-Token": adminToken}
	prepareBootNetwork(t, clients.network)
	groupsPath := computePath + "/os-server-groups"
	missing := "00000000-0000-4000-8000-000000000000"
	createGroup := func(body string) (int, http.Header, []byte) {
		t.Helper()
		return request(t, instance, http.MethodPost, groupsPath, body, auth)
	}
	createServer := func(body string) (int, http.Header, []byte) {
		t.Helper()
		return request(t, instance, http.MethodPost, computePath+"/servers", body, auth)
	}
	faultMessage := func(kind string, body []byte) string {
		t.Helper()
		var faults map[string]struct {
			Message string `json:"message"`
		}
		require.NoError(t, json.Unmarshal(body, &faults))
		return faults[kind].Message
	}
	groupFields := func(body []byte) map[string]json.RawMessage {
		t.Helper()
		var wrapped map[string]map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(body, &wrapped))
		require.Contains(t, wrapped, "server_group")
		return wrapped["server_group"]
	}

	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/schemas/server_groups.py#L22-L58
	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/schemas/server_groups.py#L105-L138
	status, headers, body := createGroup(`{"server_group":{"name":"web","policies":["affinity"]}}`)

	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	fields := groupFields(body)
	assert.ElementsMatch(t, []string{"id", "name", "policies", "members", "metadata"}, keysOf(fields))
	assert.JSONEq(t, `"web"`, string(fields["name"]))
	assert.JSONEq(t, `["affinity"]`, string(fields["policies"]))
	assert.JSONEq(t, `[]`, string(fields["members"]))
	assert.JSONEq(t, `{}`, string(fields["metadata"]))
	var groupID string
	require.NoError(t, json.Unmarshal(fields["id"], &groupID))

	for _, body := range []string{
		`{"server_group":{"name":"web","policies":["soft-affinity"]}}`,
		`{"server_group":{"name":"web","policies":["soft-anti-affinity"]}}`,
		`{"server_group":{"name":"web","policies":["legacy"]}}`,
		`{"server_group":{"name":"web","policies":["affinity","anti-affinity"]}}`,
		`{"server_group":{"name":"web","policies":["affinity","affinity"]}}`,
		`{"server_group":{"name":"web","policies":[]}}`,
		`{"server_group":{"name":"web","policies":"affinity"}}`,
		`{"server_group":{"name":"web"}}`,
		`{"server_group":{"policies":["affinity"]}}`,
		`{"server_group":{"name":"","policies":["affinity"]}}`,
		`{"server_group":{"name":" web","policies":["affinity"]}}`,
		`{"server_group":{"name":"web","policies":["affinity"],"metadata":{}}}`,
		`{"server_group":{"name":"web","policy":"affinity"}}`,
		`{"server_group":{"name":"web","policies":["anti-affinity"],"rules":{"max_server_per_host":2}}}`,
		`{"server_group":{"name":"web","policies":["affinity"]},"extra":true}`,
		`{}`,
	} {
		status, headers, response := createGroup(body)
		requireNovaFault(t, http.StatusBadRequest, "badRequest", status, headers, response)
	}

	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/server_groups.py#L133-L166
	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/exception.py#L1646-L1647
	status, headers, body = request(t, instance, http.MethodGet, groupsPath+"/"+groupID, "", auth)

	require.Equal(t, http.StatusOK, status, "%s", body)
	assert.ElementsMatch(t, []string{"id", "name", "policies", "members", "metadata"}, keysOf(groupFields(body)))
	for _, method := range []string{http.MethodGet, http.MethodDelete} {
		status, headers, body = request(t, instance, method, groupsPath+"/"+missing, "", auth)
		requireNovaFault(t, http.StatusNotFound, "itemNotFound", status, headers, body)
		assert.Equal(t, "Instance group "+missing+" could not be found.", faultMessage("itemNotFound", body))
	}
	status, headers, body = request(t, instance, http.MethodGet, groupsPath+"/not-a-uuid", "", auth)
	requireNovaFault(t, http.StatusNotFound, "itemNotFound", status, headers, body)

	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L1667-L1678
	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/servers.py#L747-L752
	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/servers.py#L890-L902
	serverBody := func(hints string) string {
		return `{"server":{"name":"web","imageRef":"` + fixtureImageID + `","flavorRef":"1"},` + hints + `}`
	}
	status, headers, body = createServer(serverBody(`"os:scheduler_hints":{"group":"` + missing + `"}`))
	requireNovaFault(t, http.StatusBadRequest, "badRequest", status, headers, body)
	assert.Equal(t, "Instance group "+missing+" could not be found.", faultMessage("badRequest", body))
	for _, hints := range []string{
		`"os:scheduler_hints":{"group":"web"}`,
		`"os:scheduler_hints":{"group":["` + groupID + `"]}`,
		`"os:scheduler_hints":{"group":null}`,
		`"os:scheduler_hints":{"same_host":"` + fixtureServerID + `"}`,
		`"os:scheduler_hints":"group"`,
		`"OS-SCH-HNT:scheduler_hints":{"group":"web"}`,
	} {
		status, headers, body = createServer(serverBody(hints))
		requireNovaFault(t, http.StatusBadRequest, "badRequest", status, headers, body)
	}
	status, _, body = createServer(serverBody(`"OS-SCH-HNT:scheduler_hints":{"group":"` + groupID + `"}`))
	require.Equal(t, http.StatusAccepted, status, "%s", body)
	status, _, body = createServer(serverBody(`"os:scheduler_hints":{}`))
	require.Equal(t, http.StatusAccepted, status, "%s", body)
	status, _, body = request(t, instance, http.MethodGet, groupsPath+"/"+groupID, "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	var members []string
	require.NoError(t, json.Unmarshal(groupFields(body)["members"], &members))
	assert.Len(t, members, 1)

	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/servers.py#L835-L837
	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/conf/quota.py#L173-L183
	for i := range 9 {
		status, _, body = createServer(serverBody(`"os:scheduler_hints":{"group":"` + groupID + `"}`))
		require.Equal(t, http.StatusAccepted, status, "member %d: %s", i, body)
	}
	status, headers, body = createServer(serverBody(`"os:scheduler_hints":{"group":"` + groupID + `"}`))
	requireNovaFault(t, http.StatusForbidden, "forbidden", status, headers, body)
	assert.Equal(t, "Quota exceeded, too many servers in group", faultMessage("forbidden", body))

	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/server_groups.py#L168-L206
	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/schemas/server_groups.py#L79-L91
	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/common.py#L215-L231
	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/conf/quota.py#L157-L172
	for i := range 9 {
		status, _, body = createGroup(fmt.Sprintf(`{"server_group":{"name":"group-%d","policies":["anti-affinity"]}}`, i))
		require.Equal(t, http.StatusOK, status, "group %d: %s", i, body)
	}
	status, headers, body = createGroup(`{"server_group":{"name":"over","policies":["affinity"]}}`)
	requireNovaFault(t, http.StatusForbidden, "forbidden", status, headers, body)
	assert.Equal(t, "Quota exceeded, too many server groups.", faultMessage("forbidden", body))
	listNames := func(query string) []string {
		t.Helper()
		status, _, body := request(t, instance, http.MethodGet, groupsPath+query, "", auth)
		require.Equal(t, http.StatusOK, status, "%s", body)
		var listed struct {
			ServerGroups []map[string]json.RawMessage `json:"server_groups"`
		}
		require.NoError(t, json.Unmarshal(body, &listed))
		names := make([]string, len(listed.ServerGroups))
		for i, group := range listed.ServerGroups {
			assert.ElementsMatch(t, []string{"id", "name", "policies", "members", "metadata"}, keysOf(group))
			require.NoError(t, json.Unmarshal(group["name"], &names[i]))
		}
		return names
	}
	assert.Len(t, listNames(""), 10)
	assert.Equal(t, listNames(""), listNames("?all_projects=True&unknown=1"))
	assert.Equal(t, []string{"group-1", "group-2"}, listNames("?offset=2&limit=2"))
	assert.Equal(t, []string{"group-8"}, listNames("?offset=9&limit=0"))
	assert.Empty(t, listNames("?offset=10"))
	for _, query := range []string{"?limit=-1", "?offset=abc", "?limit=", "?limit=1&limit=x"} {
		status, headers, body = request(t, instance, http.MethodGet, groupsPath+query, "", auth)
		requireNovaFault(t, http.StatusBadRequest, "badRequest", status, headers, body)
	}

	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/server_groups.py#L151-L166
	status, headers, body = request(t, instance, http.MethodDelete, groupsPath+"/"+groupID, "", auth)

	assert.Equal(t, http.StatusNoContent, status, "%s", body)
	assert.Empty(t, body)
	assert.Empty(t, headers.Get("Content-Type"))
	status, _, _ = request(t, instance, http.MethodGet, groupsPath+"/"+groupID, "", auth)
	assert.Equal(t, http.StatusNotFound, status)

	status, _, body = request(t, instance, http.MethodPost, "/_sandstack/reset", "", admin)
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	compute := newBootClients(t, instance).compute
	assert.Empty(t, listServerGroups(t, compute))
}
