package server_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/security/groups"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGophercloudCreatesAndDeletesCustomNeutronSecurityGroups(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	projectID := routerProjectID(t, provider)
	client, err := openstack.NewNetworkV2(provider, gophercloud.EndpointOpts{
		Region: "RegionOne", Availability: gophercloud.AvailabilityPublic,
	})
	require.NoError(t, err)
	ctx := t.Context()
	list := func(name string) []groups.SecGroup {
		t.Helper()
		pages, err := groups.List(client, groups.ListOpts{Name: name, ProjectID: projectID}).AllPages(ctx)
		require.NoError(t, err)
		result, err := groups.ExtractGroups(pages)
		require.NoError(t, err)
		return result
	}
	defaultGroups := list("default")
	require.Len(t, defaultGroups, 1)
	const name = "application traffic"
	opts := groups.CreateOpts{Name: name, Description: "example security group", ProjectID: projectID}

	first, err := groups.Create(ctx, client, opts).Extract()
	require.NoError(t, err)
	second, err := groups.Create(ctx, client, opts).Extract()
	require.NoError(t, err)

	require.NotEmpty(t, first.ID)
	require.NotEmpty(t, second.ID)
	assert.NotEqual(t, first.ID, second.ID)
	assert.ElementsMatch(t, []groups.SecGroup{*first, *second}, list(name))
	for _, group := range []*groups.SecGroup{first, second} {
		assert.Equal(t, name, group.Name)
		assert.Equal(t, opts.Description, group.Description)
		assert.Equal(t, projectID, group.TenantID)
		assert.Equal(t, projectID, group.ProjectID)
		assert.True(t, group.Stateful)
		read, err := groups.Get(ctx, client, group.ID).Extract()
		require.NoError(t, err)
		assert.Equal(t, group, read)
		require.Len(t, group.Rules, 2)
		etherTypes := make([]string, 0, len(group.Rules))
		for _, rule := range group.Rules {
			require.NotEmpty(t, rule.ID)
			assert.Equal(t, group.ID, rule.SecGroupID)
			assert.Equal(t, "egress", rule.Direction)
			assert.Empty(t, rule.Protocol)
			assert.Empty(t, rule.RemoteGroupID)
			assert.Empty(t, rule.RemoteIPPrefix)
			etherTypes = append(etherTypes, rule.EtherType)
		}
		assert.ElementsMatch(t, []string{"IPv4", "IPv6"}, etherTypes)
	}

	require.NoError(t, groups.Delete(ctx, client, first.ID).ExtractErr())
	_, err = groups.Get(ctx, client, first.ID).Extract()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusNotFound), "deleted group: %v", err)
	assert.Equal(t, []groups.SecGroup{*second}, list(name))
	assert.Equal(t, defaultGroups, list("default"))
}

func TestNeutronCustomSecurityGroupLifecycleWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	projectID := routerProjectID(t, provider)
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	create := func(payload string, expected int) []byte {
		t.Helper()
		status, headers, body := request(t, instance, http.MethodPost, securityGroupsPath, payload, auth)
		require.Equal(t, expected, status, "%s: %s", payload, body)
		requireJSON(t, headers)
		return body
	}

	// https://docs.openstack.org/api-ref/network/v2/#create-security-group
	// https://docs.openstack.org/api-ref/network/v2/#delete-security-group
	for _, payload := range []string{
		`{"security_group":{"name":null}}`,
		`{"security_group":{"name":"invalid owner","project_id":"00000000-0000-0000-0000-000000000000"}}`,
	} {
		create(payload, http.StatusBadRequest)
	}
	assertPortNeutronError(t, create(`{"security_group":{"name":"DeFaUlT"}}`, http.StatusConflict), "SecurityGroupDefaultAlreadyExists")
	empty := create(`{"security_group":{}}`, http.StatusCreated)
	var defaultFields struct {
		Group map[string]json.RawMessage `json:"security_group"`
	}
	require.NoError(t, json.Unmarshal(empty, &defaultFields))
	assert.JSONEq(t, `""`, string(defaultFields.Group["name"]))
	assert.JSONEq(t, `""`, string(defaultFields.Group["description"]))
	assert.JSONEq(t, "true", string(defaultFields.Group["stateful"]))
	created := create(`{"security_group":{"name":"wire group","stateful":false}}`, http.StatusCreated)
	var group struct {
		Group map[string]json.RawMessage `json:"security_group"`
	}
	require.NoError(t, json.Unmarshal(created, &group))
	var id, owner, tenant string
	require.NoError(t, json.Unmarshal(group.Group["id"], &id))
	require.NotEmpty(t, id)
	require.NoError(t, json.Unmarshal(group.Group["project_id"], &owner))
	require.NoError(t, json.Unmarshal(group.Group["tenant_id"], &tenant))
	assert.Equal(t, projectID, owner)
	assert.Equal(t, projectID, tenant)
	assert.JSONEq(t, `"wire group"`, string(group.Group["name"]))
	assert.JSONEq(t, `""`, string(group.Group["description"]))
	assert.JSONEq(t, "false", string(group.Group["stateful"]))
	var rules []map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(group.Group["security_group_rules"], &rules))
	require.Len(t, rules, 2)
	for _, rule := range rules {
		assert.JSONEq(t, `"egress"`, string(rule["direction"]))
		for _, field := range []string{
			"protocol", "port_range_min", "port_range_max", "remote_ip_prefix",
			"remote_group_id", "remote_address_group_id", "normalized_cidr",
		} {
			assert.JSONEq(t, "null", string(rule[field]), field)
		}
	}

	status, _, body := request(t, instance, http.MethodGet, securityGroupsPath+"?name=default", "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	var defaults struct {
		Groups []struct {
			ID string `json:"id"`
		} `json:"security_groups"`
	}
	require.NoError(t, json.Unmarshal(body, &defaults))
	require.Len(t, defaults.Groups, 1)
	status, _, body = request(t, instance, http.MethodDelete, securityGroupsPath+"/"+defaults.Groups[0].ID, "", auth)
	require.Equal(t, http.StatusConflict, status, "%s", body)
	assertPortNeutronError(t, body, "SecurityGroupCannotRemoveDefault")

	path := securityGroupsPath + "/" + id
	status, _, body = request(t, instance, http.MethodDelete, path, "", nil)
	assert.Equal(t, http.StatusUnauthorized, status, "%s", body)
	status, _, body = request(t, instance, http.MethodDelete, path, "", auth)
	assert.Equal(t, http.StatusNoContent, status, "%s", body)
	assert.Empty(t, body)
	status, _, body = request(t, instance, http.MethodGet, path, "", auth)
	assert.Equal(t, http.StatusNotFound, status, "%s", body)
}
