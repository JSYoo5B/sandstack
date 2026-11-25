package server_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/security/groups"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/security/rules"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const securityGroupRulesPath = networkPath + "/v2.0/security-group-rules"

func TestGophercloudReadsNeutronSecurityGroupRules(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	client, err := openstack.NewNetworkV2(provider, gophercloud.EndpointOpts{
		Region:       "RegionOne",
		Availability: gophercloud.AvailabilityPublic,
	})
	require.NoError(t, err)
	ctx := t.Context()
	pages, err := groups.List(client, groups.ListOpts{Name: "default"}).AllPages(ctx)
	require.NoError(t, err)
	defaultGroups, err := groups.ExtractGroups(pages)
	require.NoError(t, err)
	require.Len(t, defaultGroups, 1)
	baseline := defaultGroups[0]
	custom, err := groups.Create(ctx, client, groups.CreateOpts{Name: "rule membership"}).Extract()
	require.NoError(t, err)

	list := func(opts rules.ListOpts) []rules.SecGroupRule {
		t.Helper()
		pages, err := rules.List(client, opts).AllPages(ctx)
		require.NoError(t, err)
		items, err := rules.ExtractRules(pages)
		require.NoError(t, err)
		return items
	}
	assert.ElementsMatch(t, baseline.Rules, list(rules.ListOpts{SecGroupID: baseline.ID}))
	assert.ElementsMatch(t, custom.Rules, list(rules.ListOpts{SecGroupID: custom.ID}))
	require.Len(t, custom.Rules, 2)
	allRules := append(append([]rules.SecGroupRule{}, baseline.Rules...), custom.Rules...)
	for _, rule := range allRules {
		item, err := rules.Get(ctx, client, rule.ID).Extract()
		require.NoError(t, err)
		assert.Equal(t, rule, *item)
	}
	for _, rule := range custom.Rules {
		assert.Equal(t, []rules.SecGroupRule{rule}, list(rules.ListOpts{
			SecGroupID: custom.ID, Direction: "egress", EtherType: rule.EtherType,
		}))
	}
	assert.Empty(t, list(rules.ListOpts{SecGroupID: custom.ID, Direction: "ingress"}))
	_, err = rules.Get(ctx, client, "00000000-0000-0000-0000-000000000000").Extract()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusNotFound), "%v", err)

	require.NoError(t, groups.Delete(ctx, client, custom.ID).ExtractErr())
	assert.Empty(t, list(rules.ListOpts{SecGroupID: custom.ID}))
	for _, rule := range custom.Rules {
		_, err = rules.Get(ctx, client, rule.ID).Extract()
		assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusNotFound), "%v", err)
	}
	assert.ElementsMatch(t, baseline.Rules, list(rules.ListOpts{}))
}

func TestNeutronSecurityGroupRuleReadWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	project := routerProjectID(t, provider)
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	readGroupID := func(body []byte, key string) string {
		t.Helper()
		var response map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(body, &response))
		type group struct {
			ID string `json:"id"`
		}
		var record group
		if key == "security_groups" {
			var listed []group
			require.NoError(t, json.Unmarshal(response[key], &listed))
			require.Len(t, listed, 1)
			record = listed[0]
		} else {
			require.NoError(t, json.Unmarshal(response[key], &record))
		}
		require.NotEmpty(t, record.ID)
		return record.ID
	}
	status, _, body := request(t, instance, http.MethodGet, securityGroupsPath+"?name=default", "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	defaultID := readGroupID(body, "security_groups")
	status, _, body = request(t, instance, http.MethodPost, securityGroupsPath,
		`{"security_group":{"name":"wire rule membership"}}`, auth)
	require.Equal(t, http.StatusCreated, status, "%s", body)
	customID := readGroupID(body, "security_group")

	query := url.Values{
		"security_group_id": {customID, defaultID},
		"ethertype":         {"iPv4"},
		"direction":         {"egress"},
		"project_id":        {"another-project", project},
		"tenant_id":         {project},
	}
	// https://docs.openstack.org/api-ref/network/v2/#list-security-group-rules
	// https://docs.openstack.org/api-ref/network/v2/#show-security-group-rule-details
	status, headers, body := request(t, instance, http.MethodGet, securityGroupRulesPath+"?"+query.Encode(), "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	var response map[string][]json.RawMessage
	require.NoError(t, json.Unmarshal(body, &response))
	require.Len(t, response, 1)
	list := response["security_group_rules"]
	require.Len(t, list, 2)
	belonging := make(map[string]bool)
	for _, raw := range list {
		var rule map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(raw, &rule))
		var id, groupID string
		require.NoError(t, json.Unmarshal(rule["id"], &id))
		require.NoError(t, json.Unmarshal(rule["security_group_id"], &groupID))
		require.NotEmpty(t, id)
		assert.NotContains(t, belonging, groupID)
		var defaultGroup *bool
		require.NoError(t, json.Unmarshal(rule["belongs_to_default_sg"], &defaultGroup))
		require.NotNil(t, defaultGroup)
		belonging[groupID] = *defaultGroup
		assert.Equal(t, `"IPv4"`, string(rule["ethertype"]))
		assert.Equal(t, `"egress"`, string(rule["direction"]))
		for _, field := range []string{
			"protocol", "port_range_min", "port_range_max", "remote_group_id",
			"remote_ip_prefix", "normalized_cidr", "remote_address_group_id",
		} {
			assert.Equal(t, "null", string(rule[field]), field)
		}
		var projectID, tenantID string
		require.NoError(t, json.Unmarshal(rule["project_id"], &projectID))
		require.NoError(t, json.Unmarshal(rule["tenant_id"], &tenantID))
		assert.Equal(t, project, projectID)
		assert.Equal(t, project, tenantID)

		status, headers, detail := request(t, instance, http.MethodGet, securityGroupRulesPath+"/"+id, "", auth)
		require.Equal(t, http.StatusOK, status, "%s", detail)
		requireJSON(t, headers)
		assert.JSONEq(t, `{"security_group_rule":`+string(raw)+`}`, string(detail))
	}
	assert.Equal(t, map[string]bool{defaultID: true, customID: false}, belonging)

	query.Set("tenant_id", "another-project")
	status, _, body = request(t, instance, http.MethodGet, securityGroupRulesPath+"?"+query.Encode(), "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	assert.JSONEq(t, `{"security_group_rules":[]}`, string(body))

	status, _, body = request(t, instance, http.MethodGet, securityGroupRulesPath+"?ethertype=IPX", "", auth)
	assert.Equal(t, http.StatusBadRequest, status, "%s", body)
	status, _, body = request(t, instance, http.MethodGet, securityGroupRulesPath, "", nil)
	assert.Equal(t, http.StatusUnauthorized, status, "%s", body)
}
