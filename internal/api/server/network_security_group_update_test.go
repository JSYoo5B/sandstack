package server_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/security/groups"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/ports"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGophercloudUpdatesNeutronSecurityGroups(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	resources := createRouterInterfaceResources(t, provider)
	client := resources.service
	ctx := t.Context()
	custom, err := groups.Create(ctx, client, groups.CreateOpts{Name: "web", Description: "before"}).Extract()
	require.NoError(t, err)
	defaultPages, err := groups.List(client, groups.ListOpts{Name: "default"}).AllPages(ctx)
	require.NoError(t, err)
	defaults, err := groups.ExtractGroups(defaultPages)
	require.NoError(t, err)
	require.Len(t, defaults, 1)
	description, stateless, stateful := "after", false, true

	// https://docs.openstack.org/api-ref/network/v2/#update-security-group
	updated, err := groups.Update(ctx, client, custom.ID, groups.UpdateOpts{
		Name: "web tier", Description: &description, Stateful: &stateless,
	}).Extract()
	require.NoError(t, err)
	read, err := groups.Get(ctx, client, custom.ID).Extract()
	require.NoError(t, err)
	pages, err := groups.List(client, groups.ListOpts{Name: "web tier"}).AllPages(ctx)
	require.NoError(t, err)
	listed, err := groups.ExtractGroups(pages)
	require.NoError(t, err)

	assert.Equal(t, custom.ID, updated.ID)
	assert.Equal(t, "web tier", updated.Name)
	assert.Equal(t, description, updated.Description)
	assert.False(t, updated.Stateful)
	assert.Equal(t, custom.Rules, updated.Rules)
	assert.Equal(t, updated, read)
	assert.Equal(t, []groups.SecGroup{*updated}, listed)

	bound := []string{custom.ID}
	_, err = ports.Create(ctx, client, ports.CreateOpts{
		NetworkID: resources.networkID, SecurityGroups: &bound,
	}).Extract()
	require.NoError(t, err)
	unchangedMode, err := groups.Update(ctx, client, custom.ID, groups.UpdateOpts{
		Name: "bound web tier", Stateful: &stateless,
	}).Extract()
	require.NoError(t, err)
	assert.Equal(t, "bound web tier", unchangedMode.Name)

	for _, tc := range []struct {
		name    string
		groupID string
		opts    groups.UpdateOpts
		status  int
	}{
		{name: "bound mode change", groupID: custom.ID, opts: groups.UpdateOpts{Stateful: &stateful}, status: http.StatusConflict},
		{name: "reserved name", groupID: custom.ID, opts: groups.UpdateOpts{Name: "Default"}, status: http.StatusConflict},
		{name: "default rename", groupID: defaults[0].ID, opts: groups.UpdateOpts{Name: "renamed"}, status: http.StatusConflict},
		{
			name: "missing group", groupID: "00000000-0000-0000-0000-000000000000",
			opts: groups.UpdateOpts{Name: "missing"}, status: http.StatusNotFound,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := groups.Update(ctx, client, tc.groupID, tc.opts).Extract()

			assert.True(t, gophercloud.ResponseCodeIs(err, tc.status), "%v", err)
		})
	}
	current, err := groups.Get(ctx, client, custom.ID).Extract()
	require.NoError(t, err)
	assert.Equal(t, unchangedMode, current)

	defaultDescription := "described default"
	described, err := groups.Update(ctx, client, defaults[0].ID, groups.UpdateOpts{
		Description: &defaultDescription,
	}).Extract()
	require.NoError(t, err)
	assert.Equal(t, "default", described.Name)
	assert.Equal(t, defaultDescription, described.Description)
}

func TestNeutronSecurityGroupUpdateWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	status, _, body := request(t, instance, http.MethodPost, securityGroupsPath,
		`{"security_group":{"name":"wire","description":"kept"}}`, auth)
	require.Equal(t, http.StatusCreated, status, "%s", body)
	var created struct {
		Group struct {
			ID string `json:"id"`
		} `json:"security_group"`
	}
	require.NoError(t, json.Unmarshal(body, &created))
	path := securityGroupsPath + "/" + created.Group.ID
	update := func(payload string, expected int) map[string]json.RawMessage {
		t.Helper()
		status, headers, body := request(t, instance, http.MethodPut, path, payload, auth)
		require.Equal(t, expected, status, "%s: %s", payload, body)
		requireJSON(t, headers)
		var response struct {
			Group map[string]json.RawMessage `json:"security_group"`
		}
		if expected == http.StatusOK {
			require.NoError(t, json.Unmarshal(body, &response))
		}
		return response.Group
	}

	// https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/extensions/securitygroup.py#L198-L232
	// https://github.com/openstack/neutron-lib/blob/master/neutron_lib/api/definitions/stateful_security_group.py#L41-L50
	for _, payload := range []string{
		`{"security_group":{"name":null}}`,
		`{"security_group":{"stateful":"maybe"}}`,
		`{"security_group":{"project_id":"00000000-0000-0000-0000-000000000000"}}`,
		`{"security_group":{"tenant_id":"00000000-0000-0000-0000-000000000000"}}`,
		`{"security_group":{"security_group_rules":[]}}`,
		`{"security_group":{"name":"a","name":"b"}}`,
		`{"group":{}}`,
	} {
		update(payload, http.StatusBadRequest)
	}
	unchanged := update(`{"security_group":{}}`, http.StatusOK)
	assert.JSONEq(t, `"wire"`, string(unchanged["name"]))
	assert.JSONEq(t, `"kept"`, string(unchanged["description"]))
	assert.JSONEq(t, "true", string(unchanged["stateful"]))
	changed := update(`{"security_group":{"name":"","stateful":"False"}}`, http.StatusOK)
	assert.JSONEq(t, `""`, string(changed["name"]))
	assert.JSONEq(t, `"kept"`, string(changed["description"]))
	assert.JSONEq(t, "false", string(changed["stateful"]))
	assert.JSONEq(t, `"`+created.Group.ID+`"`, string(changed["id"]))

	status, _, body = request(t, instance, http.MethodPut, path, `{"security_group":{"name":"dEfAuLt"}}`, auth)
	require.Equal(t, http.StatusConflict, status, "%s", body)
	assertPortNeutronError(t, body, "SecurityGroupDefaultAlreadyExists")
	status, _, body = request(t, instance, http.MethodPut, path, `{"security_group":{}}`, nil)
	assert.Equal(t, http.StatusUnauthorized, status, "%s", body)
}

func TestNeutronSecurityGroupUpdateConflictErrors(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	status, _, body := request(t, instance, http.MethodGet, securityGroupsPath+"?name=default", "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	var defaults struct {
		Groups []struct {
			ID string `json:"id"`
		} `json:"security_groups"`
	}
	require.NoError(t, json.Unmarshal(body, &defaults))
	require.Len(t, defaults.Groups, 1)

	// https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/db/securitygroups_db.py#L303-L344
	status, _, body = request(t, instance, http.MethodPut, securityGroupsPath+"/"+defaults.Groups[0].ID,
		`{"security_group":{"name":"renamed"}}`, auth)

	require.Equal(t, http.StatusConflict, status, "%s", body)
	assertPortNeutronError(t, body, "SecurityGroupCannotUpdateDefault")
}
