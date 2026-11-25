package server_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/security/groups"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const securityGroupsPath = networkPath + "/v2.0/security-groups"

func TestGophercloudDiscoversDefaultNeutronSecurityGroup(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	projectID := routerProjectID(t, provider)
	network, err := openstack.NewNetworkV2(provider, gophercloud.EndpointOpts{
		Region: "RegionOne", Availability: gophercloud.AvailabilityPublic,
	})
	require.NoError(t, err)
	ctx := t.Context()

	pages, err := groups.List(network, groups.ListOpts{Name: "default", ProjectID: projectID}).AllPages(ctx)
	require.NoError(t, err)
	listed, err := groups.ExtractGroups(pages)
	require.NoError(t, err)
	require.Len(t, listed, 1)

	group := listed[0]
	require.NotEmpty(t, group.ID)
	assert.Equal(t, "default", group.Name)
	assert.Equal(t, projectID, group.ProjectID)
	assert.Equal(t, projectID, group.TenantID)
	assert.True(t, group.Stateful)
	detail, err := groups.Get(ctx, network, group.ID).Extract()
	require.NoError(t, err)
	assert.Equal(t, &group, detail)

	require.Len(t, group.Rules, 4)
	directions := make(map[[2]string]string, len(group.Rules))
	ids := make(map[string]struct{}, len(group.Rules))
	for _, rule := range group.Rules {
		require.NotEmpty(t, rule.ID)
		assert.NotContains(t, ids, rule.ID)
		ids[rule.ID] = struct{}{}
		assert.Equal(t, group.ID, rule.SecGroupID)
		assert.Equal(t, projectID, rule.ProjectID)
		assert.Equal(t, projectID, rule.TenantID)
		assert.Empty(t, rule.Protocol)
		assert.Zero(t, rule.PortRangeMin)
		assert.Zero(t, rule.PortRangeMax)
		assert.Empty(t, rule.RemoteIPPrefix)
		directions[[2]string{rule.Direction, rule.EtherType}] = rule.RemoteGroupID
	}
	assert.Equal(t, map[[2]string]string{
		{"egress", "IPv4"}:  "",
		{"egress", "IPv6"}:  "",
		{"ingress", "IPv4"}: group.ID,
		{"ingress", "IPv6"}: group.ID,
	}, directions)
}

func TestNeutronDefaultSecurityGroupWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	projectID := routerProjectID(t, provider)
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	query := url.Values{
		"name": {"unmatched", "default"}, "project_id": {projectID}, "tenant_id": {projectID},
	}

	// https://docs.openstack.org/api-ref/network/v2/#list-security-groups
	// https://docs.openstack.org/api-ref/network/v2/#show-security-group-details
	status, headers, body := request(t, instance, http.MethodGet, securityGroupsPath+"?"+query.Encode(), "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	var list struct {
		Groups []json.RawMessage `json:"security_groups"`
	}
	require.NoError(t, json.Unmarshal(body, &list))
	require.Len(t, list.Groups, 1)
	var group struct {
		ID        string                       `json:"id"`
		Name      string                       `json:"name"`
		Stateful  *bool                        `json:"stateful"`
		Shared    *bool                        `json:"shared"`
		ProjectID string                       `json:"project_id"`
		TenantID  string                       `json:"tenant_id"`
		Rules     []map[string]json.RawMessage `json:"security_group_rules"`
	}
	require.NoError(t, json.Unmarshal(list.Groups[0], &group))
	require.NotEmpty(t, group.ID)
	assert.Equal(t, "default", group.Name)
	require.NotNil(t, group.Stateful)
	assert.True(t, *group.Stateful)
	require.NotNil(t, group.Shared)
	assert.False(t, *group.Shared)
	assert.Equal(t, projectID, group.ProjectID)
	assert.Equal(t, projectID, group.TenantID)
	require.Len(t, group.Rules, 4)
	for _, rule := range group.Rules {
		for _, field := range []string{
			"protocol", "port_range_min", "port_range_max", "remote_ip_prefix", "remote_address_group_id", "normalized_cidr",
		} {
			assert.JSONEq(t, "null", string(rule[field]), field)
		}
		assert.JSONEq(t, `""`, string(rule["description"]))
		if string(rule["direction"]) == `"egress"` {
			assert.JSONEq(t, "null", string(rule["remote_group_id"]))
		} else {
			var remote string
			require.NoError(t, json.Unmarshal(rule["remote_group_id"], &remote))
			assert.Equal(t, group.ID, remote)
		}
	}

	path := securityGroupsPath + "/" + group.ID
	status, headers, body = request(t, instance, http.MethodGet, path, "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	assert.JSONEq(t, `{"security_group":`+string(list.Groups[0])+`}`, string(body))

	query.Set("tenant_id", "not-the-project")
	status, _, body = request(t, instance, http.MethodGet, securityGroupsPath+"?"+query.Encode(), "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	assert.JSONEq(t, `{"security_groups":[`+string(list.Groups[0])+`]}`, string(body))
	query.Set("tenant_id", projectID)
	query.Set("project_id", "not-the-project")
	status, _, body = request(t, instance, http.MethodGet, securityGroupsPath+"?"+query.Encode(), "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	assert.JSONEq(t, `{"security_groups":[]}`, string(body))
	status, _, body = request(t, instance, http.MethodGet, securityGroupsPath+"/00000000-0000-0000-0000-000000000000", "", auth)
	assert.Equal(t, http.StatusNotFound, status, "%s", body)
	status, _, body = request(t, instance, http.MethodGet, path, "", nil)
	assert.Equal(t, http.StatusUnauthorized, status, "%s", body)
}
