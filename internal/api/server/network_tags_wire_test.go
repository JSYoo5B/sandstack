package server_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/layer3/floatingips"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/security/groups"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/networks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const unknownTagResourceID = "00000000-0000-4000-8000-000000000000"

func TestNeutronTagsWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	resources := createFloatingIPResources(t, provider)
	client := resources.service
	ctx := t.Context()
	group, err := groups.Create(ctx, client, groups.CreateOpts{Name: "wire"}).Extract()
	require.NoError(t, err)
	floatingIP, err := floatingips.Create(ctx, client, floatingips.CreateOpts{
		FloatingNetworkID: resources.externalNetworkID,
	}).Extract()
	require.NoError(t, err)
	externalNetwork, err := networks.Get(ctx, client, resources.externalNetworkID).Extract()
	require.NoError(t, err)
	require.Len(t, externalNetwork.Subnets, 1)
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	send := func(method, path, body string) (int, http.Header, []byte) {
		t.Helper()
		return request(t, instance, method, path, body, auth)
	}
	assertNeutronError := func(t *testing.T, body []byte, kind, message string) {
		t.Helper()
		assert.JSONEq(t,
			fmt.Sprintf(`{"NeutronError":{"type":%q,"message":%q,"detail":""}}`, kind, message),
			string(body),
		)
	}
	collections := []struct {
		path       string
		collection string
		member     string
		id         string
	}{
		{networksPath, "networks", "network", resources.networkID},
		{subnetsPath, "subnets", "subnet", resources.subnetID},
		{portsPath, "ports", "port", resources.portID},
		{networkPath + "/v2.0/routers", "routers", "router", resources.routerID},
		{securityGroupsPath, "security_groups", "security_group", group.ID},
		{floatingIPsPath, "floatingips", "floatingip", floatingIP.ID},
	}

	// https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/extensions/tagging.py#L206-L323
	// https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/extensions/tagging.py#L345-L365
	// https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/pecan_wsgi/controllers/utils.py#L91-L110
	for _, tc := range collections {
		t.Run(tc.collection+" lifecycle", func(t *testing.T) {
			tags := tc.path + "/" + tc.id + "/tags"

			replaceStatus, _, replaced := send(http.MethodPut, tags, `{"tags":["red","blue",""]}`)
			listStatus, listHeaders, listed := send(http.MethodGet, tags, "")
			addStatus, _, added := send(http.MethodPut, tags+"/green", "")
			repeatStatus, _, _ := send(http.MethodPut, tags+"/green", "")
			showStatus, _, shown := send(http.MethodGet, tc.path+"/"+tc.id, "")
			checkStatus, _, checked := send(http.MethodGet, tags+"/green", "")
			removeStatus, _, removed := send(http.MethodDelete, tags+"/green", "")
			clearStatus, _, cleared := send(http.MethodDelete, tags, "")
			_, _, empty := send(http.MethodGet, tags, "")

			assert.Equal(t, http.StatusOK, replaceStatus, "%s", replaced)
			assert.JSONEq(t, `{"tags":["red","blue",""]}`, string(replaced))
			assert.Equal(t, http.StatusOK, listStatus)
			requireJSON(t, listHeaders)
			assert.JSONEq(t, `{"tags":["","blue","red"]}`, string(listed))
			assert.Equal(t, http.StatusCreated, addStatus, "%s", added)
			assert.Equal(t, "null", strings.TrimSpace(string(added)))
			assert.Equal(t, http.StatusCreated, repeatStatus)
			require.Equal(t, http.StatusOK, showStatus, "%s", shown)
			var resource map[string]struct {
				Tags []string `json:"tags"`
			}
			require.NoError(t, json.Unmarshal(shown, &resource))
			assert.Equal(t, []string{"", "blue", "green", "red"}, resource[tc.member].Tags)
			assert.Equal(t, http.StatusNoContent, checkStatus)
			assert.Empty(t, checked)
			assert.Equal(t, http.StatusNoContent, removeStatus)
			assert.Empty(t, removed)
			assert.Equal(t, http.StatusNoContent, clearStatus)
			assert.Empty(t, cleared)
			assert.JSONEq(t, `{"tags":[]}`, string(empty))
		})
	}

	// https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/extensions/tagging.py#L106-L125
	// https://github.com/openstack/neutron-lib/blob/5.0.0/neutron_lib/api/validators/__init__.py#L165-L209
	// https://github.com/openstack/neutron-lib/blob/5.0.0/neutron_lib/api/validators/__init__.py#L312-L346
	long := strings.Repeat("x", 256)
	limit := make([]string, 51)
	for i := range limit {
		limit[i] = fmt.Sprintf("%q", fmt.Sprintf("tag-%02d", i))
	}
	for _, tc := range []struct {
		name    string
		body    string
		message string
	}{
		{"missing body", "", "Invalid tags body"},
		{"array body", `["red"]`, "Invalid tags body"},
		{"missing tags", `{"tag":["red"]}`, "Invalid tags body"},
		{"string tags", `{"tags":"red"}`, "'red' is not a list"},
		{"object tags", `{"tags":{"red":true}}`, "'{'red': True}' is not a list"},
		{"null tags", `{"tags":null}`, "'None' is not a list"},
		{"duplicate tags", `{"tags":["red","blue","red","blue","red"]}`, "Duplicate items in the list: 'red, blue'"},
		{"number tag", `{"tags":["red",1]}`, "'1' is not a valid string"},
		{"float tag", `{"tags":[1.0]}`, "'1.0' is not a valid string"},
		{"boolean tag", `{"tags":[false]}`, "'False' is not a valid string"},
		{"null tag", `{"tags":[null]}`, "'None' is not a valid string"},
		{"list tag", `{"tags":[["red"]]}`, "'['red']' is not a valid string"},
		{"long tag", `{"tags":["` + long + `"]}`, "'" + long + "' exceeds maximum length of 255"},
	} {
		t.Run("replace "+tc.name, func(t *testing.T) {
			for _, collection := range collections {
				status, _, body := send(http.MethodPut, collection.path+"/"+collection.id+"/tags", tc.body)

				assert.Equal(t, http.StatusBadRequest, status, "%s %s", collection.collection, body)
				assertNeutronError(t, body, "InvalidInput", "Invalid input for operation: "+tc.message+".")
			}
		})
	}
	for _, collection := range collections {
		t.Run(collection.collection+" limits and absent tags", func(t *testing.T) {
			tags := collection.path + "/" + collection.id + "/tags"
			ownedFifty := `{"tags":[` + strings.Join(limit[:50], ",") + `]}`

			overStatus, _, over := send(http.MethodPut, tags, `{"tags":[`+strings.Join(limit, ",")+`]}`)
			longStatus, _, longBody := send(http.MethodPut, tags+"/"+long, "")
			fullStatus, _, full := send(http.MethodPut, tags, ownedFifty)
			existingStatus, _, existing := send(http.MethodPut, tags+"/tag-00", "")
			extraStatus, _, extra := send(http.MethodPut, tags+"/tag-50", "")
			checkStatus, _, check := send(http.MethodGet, tags+"/absent", "")
			deleteStatus, _, deleted := send(http.MethodDelete, tags+"/absent", "")
			clearStatus, _, _ := send(http.MethodDelete, tags, "")

			limitMessage := "Bad " + collection.collection +
				" request: The number of tags exceed the per-resource limit of 50."
			assert.Equal(t, http.StatusBadRequest, overStatus, "%s", over)
			assertNeutronError(t, over, "BadRequest", limitMessage)
			assert.Equal(t, http.StatusBadRequest, longStatus, "%s", longBody)
			assertNeutronError(t, longBody, "InvalidInput",
				"Invalid input for operation: '"+long+"' exceeds maximum length of 255.")
			assert.Equal(t, http.StatusOK, fullStatus, "%s", full)
			assert.Equal(t, http.StatusCreated, existingStatus, "%s", existing)
			assert.Equal(t, http.StatusBadRequest, extraStatus, "%s", extra)
			assertNeutronError(t, extra, "BadRequest", limitMessage)
			assert.Equal(t, http.StatusNotFound, checkStatus, "%s", check)
			assertNeutronError(t, check, "TagNotFound", "Tag absent could not be found.")
			assert.Equal(t, http.StatusNotFound, deleteStatus, "%s", deleted)
			assertNeutronError(t, deleted, "TagNotFound", "Tag absent could not be found.")
			assert.Equal(t, http.StatusNoContent, clearStatus)
		})
	}

	// https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/extensions/tagging.py#L151-L204
	// https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/api/api_common.py#L453-L521
	for _, collection := range collections {
		t.Run(collection.collection+" unknown resource", func(t *testing.T) {
			tags := collection.path + "/" + unknownTagResourceID + "/tags"
			for _, operation := range []struct{ method, path, body string }{
				{http.MethodGet, tags, ""},
				{http.MethodPut, tags, `{"tags":["red"]}`},
				{http.MethodDelete, tags, ""},
				{http.MethodGet, tags + "/red", ""},
				{http.MethodPut, tags + "/red", ""},
				{http.MethodDelete, tags + "/red", ""},
			} {
				status, _, body := send(operation.method, operation.path, operation.body)

				assert.Equal(t, http.StatusInternalServerError, status, "%s %s", operation.method, operation.path)
				assertNeutronError(t, body, "HTTPInternalServerError",
					"Request Failed: internal server error while processing your request.")
			}
		})
	}
	status, _, body := send(http.MethodPut, networksPath+"/"+unknownTagResourceID+"/tags", `{"tags":"red"}`)
	assert.Equal(t, http.StatusBadRequest, status, "%s", body)
	status, _, body = send(http.MethodGet, networksPath+"/"+unknownTagResourceID+"/tags/"+long, "")
	assert.Equal(t, http.StatusBadRequest, status, "%s", body)

	// https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/conf/policies/network.py#L265-L281
	// https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/conf/policies/subnet.py#L164-L172
	// https://github.com/openstack/oslo.policy/blob/6.0.0/oslo_policy/policy.py#L298-L308
	for _, tc := range []struct {
		name   string
		path   string
		member string
	}{
		{"external network", networksPath + "/" + resources.externalNetworkID, "network"},
		{"external subnet", subnetsPath + "/" + externalNetwork.Subnets[0], "subnet"},
	} {
		t.Run(tc.name+" policy", func(t *testing.T) {
			tags := tc.path + "/tags"

			readStatus, _, read := send(http.MethodGet, tags, "")
			replaceStatus, _, replaced := send(http.MethodPut, tags, `{"tags":["red"]}`)
			addStatus, _, added := send(http.MethodPut, tags+"/red", "")
			clearStatus, _, cleared := send(http.MethodDelete, tags, "")
			removeStatus, _, removed := send(http.MethodDelete, tags+"/red", "")

			assert.Equal(t, http.StatusOK, readStatus, "%s", read)
			assert.JSONEq(t, `{"tags":[]}`, string(read))
			for _, denied := range []struct {
				status int
				body   []byte
				action string
			}{
				{replaceStatus, replaced, "update"},
				{addStatus, added, "update"},
				{clearStatus, cleared, "delete"},
				{removeStatus, removed, "delete"},
			} {
				assert.Equal(t, http.StatusForbidden, denied.status, "%s", denied.body)
				assertNeutronError(t, denied.body, "PolicyNotAuthorized",
					"rule:"+denied.action+"_"+tc.member+":tags is disallowed by policy")
			}
		})
	}

	// https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/objects/tag.py#L30-L103
	// https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/api/api_common.py#L77-L118
	network := networksPath + "/" + resources.networkID
	status, _, body = send(http.MethodPut, network+"/tags", `{"tags":["a,b","","solo"]}`)
	require.Equal(t, http.StatusOK, status, "%s", body)
	for _, tc := range []struct {
		query   string
		matches bool
	}{
		{"tags=" + url.QueryEscape("a,b"), false},
		{"tags=solo&tags=", true},
		{"tags-any=" + url.QueryEscape("missing,solo"), true},
		{"not-tags=" + url.QueryEscape("solo,missing"), true},
		{"not-tags-any=" + url.QueryEscape("missing,"), false},
		{"tags=solo&not-tags-any=missing", true},
	} {
		t.Run("filter "+tc.query, func(t *testing.T) {
			status, _, body := send(http.MethodGet, networksPath+"?router:external=false&"+tc.query, "")

			require.Equal(t, http.StatusOK, status, "%s", body)
			if tc.matches {
				assert.Contains(t, string(body), resources.networkID)
			} else {
				assert.NotContains(t, string(body), resources.networkID)
			}
		})
	}
	for _, path := range []string{networksPath, subnetsPath, portsPath, networkPath + "/v2.0/routers",
		securityGroupsPath, floatingIPsPath} {
		status, _, body = send(http.MethodGet, path+"?tag=solo", "")
		assert.Equal(t, http.StatusBadRequest, status, "%s %s", path, body)
	}
	status, _, body = send(http.MethodGet, network+"/tags?fields=tags", "")
	assert.Equal(t, http.StatusBadRequest, status, "%s", body)
	assertPortNeutronError(t, body, "BadRequest")
}
