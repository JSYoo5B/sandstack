package network_test

import (
	"net/netip"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	app "github.com/JSYoo5B/sandstack/internal/app/network"
	domain "github.com/JSYoo5B/sandstack/internal/domain/network"
)

func TestSecurityGroupRuleMutationsUpdateEmbeddedRulesAndResetDefaults(t *testing.T) {
	const projectID = "owner"
	sequence := 0
	service := app.New(domain.Network{ID: "network", ProjectID: projectID}, func() string {
		sequence++
		return fixtureID(sequence)
	})
	defaults := service.ListSecurityGroups(projectID, app.SecurityGroupFilter{})
	require.Len(t, defaults, 1)
	group := defaults[0]
	require.Len(t, group.Rules, 4)
	original := group.Rules[0]
	ssh := 22
	rule := domain.SecurityGroupRule{
		Description: "SSH", Direction: "ingress", EtherType: "IPv4", Protocol: "tcp",
		PortRangeMin: &ssh, PortRangeMax: &ssh, RemoteIPPrefix: netip.MustParsePrefix("198.51.100.7/24"),
	}

	_, err := service.CreateSecurityGroupRule("foreign", group.ID, rule)
	assert.ErrorIs(t, err, app.ErrSecurityGroupRuleForbidden)
	_, err = service.CreateSecurityGroupRule(projectID, "missing", rule)
	assert.ErrorIs(t, err, app.ErrSecurityGroupNotFound)
	created, err := service.CreateSecurityGroupRule(projectID, group.ID, rule)
	require.NoError(t, err)
	assert.Equal(t, fixtureID(1), created.Rule.ID)
	assert.Equal(t, rule.RemoteIPPrefix, created.Rule.RemoteIPPrefix)
	assert.True(t, created.BelongsToDefault)

	rule.Description = "Duplicate with another description"
	_, err = service.CreateSecurityGroupRule(projectID, group.ID, rule)
	assert.ErrorIs(t, err, app.ErrSecurityGroupRuleExists)
	rule.RemoteIPPrefix = netip.MustParsePrefix("198.51.100.8/24")
	distinct, err := service.CreateSecurityGroupRule(projectID, group.ID, rule)
	require.NoError(t, err)
	assert.Equal(t, fixtureID(2), distinct.Rule.ID)

	assert.ErrorIs(t, service.DeleteSecurityGroupRule("foreign", original.ID), app.ErrSecurityGroupRuleNotFound)
	require.NoError(t, service.DeleteSecurityGroupRule(projectID, original.ID))
	_, err = service.GetSecurityGroupRule(projectID, original.ID)
	assert.ErrorIs(t, err, app.ErrSecurityGroupRuleNotFound)
	updated, err := service.GetSecurityGroup(projectID, group.ID)
	require.NoError(t, err)
	assert.Equal(t, append(group.Rules[1:], created.Rule, distinct.Rule), updated.Rules)
	assert.ErrorIs(t, service.DeleteSecurityGroup(projectID, group.ID), app.ErrSecurityGroupDefaultDelete)

	service.Reset()
	restored, err := service.GetSecurityGroup(projectID, group.ID)
	require.NoError(t, err)
	assert.Equal(t, group, restored)
	_, err = service.GetSecurityGroupRule(projectID, created.Rule.ID)
	assert.ErrorIs(t, err, app.ErrSecurityGroupRuleNotFound)
}

func TestStandaloneSecurityGroupRulesStayScopedAndTrackParentDeletion(t *testing.T) {
	const projectID = "owner"
	sequence := 0
	service := app.New(domain.Network{ID: "network", ProjectID: projectID}, func() string {
		sequence++
		return fixtureID(sequence)
	})
	defaults := service.ListSecurityGroups(projectID, app.SecurityGroupFilter{})
	require.Len(t, defaults, 1)
	require.Len(t, defaults[0].Rules, 4)
	custom, err := service.CreateSecurityGroup(projectID, "custom", "", true)
	require.NoError(t, err)
	foreign, err := service.CreateSecurityGroup("foreign", "custom", "", true)
	require.NoError(t, err)
	defaultRule, err := service.GetSecurityGroupRule(projectID, defaults[0].Rules[0].ID)
	require.NoError(t, err)
	assert.Equal(t, app.SecurityGroupRuleDetail{
		Rule: defaults[0].Rules[0], GroupID: defaults[0].ID, ProjectID: projectID, BelongsToDefault: true,
	}, defaultRule)
	customRule, err := service.GetSecurityGroupRule(projectID, custom.Rules[0].ID)
	require.NoError(t, err)
	assert.Equal(t, app.SecurityGroupRuleDetail{
		Rule: custom.Rules[0], GroupID: custom.ID, ProjectID: projectID,
	}, customRule)
	_, err = service.GetSecurityGroupRule(projectID, foreign.Rules[0].ID)
	assert.ErrorIs(t, err, app.ErrSecurityGroupRuleNotFound)
	_, err = service.GetSecurityGroupRule("foreign", defaultRule.Rule.ID)
	assert.ErrorIs(t, err, app.ErrSecurityGroupRuleNotFound)
	_, err = service.GetSecurityGroupRule(projectID, defaults[0].ID)
	assert.ErrorIs(t, err, app.ErrSecurityGroupRuleNotFound)

	filter := app.SecurityGroupRuleFilter{
		IDs:      []string{defaultRule.Rule.ID, customRule.Rule.ID, foreign.Rules[0].ID},
		GroupIDs: []string{defaults[0].ID, custom.ID, foreign.ID}, Directions: []string{"egress"},
		EtherTypes: []string{"IPv4"}, ProjectIDs: []string{projectID, "foreign"}, TenantIDs: []string{projectID},
	}
	assert.Equal(t, []app.SecurityGroupRuleDetail{customRule, defaultRule}, service.ListSecurityGroupRules(projectID, filter))
	filter.TenantIDs = []string{"foreign"}
	assert.Equal(t, []app.SecurityGroupRuleDetail{}, service.ListSecurityGroupRules(projectID, filter))
	require.NoError(t, service.DeleteSecurityGroup(projectID, custom.ID))
	_, err = service.GetSecurityGroupRule(projectID, customRule.Rule.ID)
	assert.ErrorIs(t, err, app.ErrSecurityGroupRuleNotFound)
	assert.Len(t, service.ListSecurityGroupRules(projectID, app.SecurityGroupRuleFilter{}), 4)
	service.Reset()
	assert.Empty(t, service.ListSecurityGroupRules("foreign", app.SecurityGroupRuleFilter{}))
	_, err = service.GetSecurityGroupRule("foreign", foreign.Rules[0].ID)
	assert.ErrorIs(t, err, app.ErrSecurityGroupRuleNotFound)
}

func TestSecurityGroupRulesReferenceRemoteGroupsAndCompareProtocolNumbers(t *testing.T) {
	const projectID = "owner"
	sequence := 0
	service := app.New(domain.Network{ID: "network", ProjectID: projectID}, func() string {
		sequence++
		return fixtureID(sequence)
	})
	web, err := service.CreateSecurityGroup(projectID, "web", "", true)
	require.NoError(t, err)
	database, err := service.CreateSecurityGroup(projectID, "database", "", true)
	require.NoError(t, err)
	foreign, err := service.CreateSecurityGroup("foreign", "web", "", true)
	require.NoError(t, err)
	postgres, echo := 5432, 8
	fromWeb := domain.SecurityGroupRule{
		Direction: "ingress", EtherType: "IPv4", Protocol: "tcp",
		PortRangeMin: &postgres, PortRangeMax: &postgres, RemoteGroupID: web.ID,
	}
	ping := domain.SecurityGroupRule{Direction: "ingress", EtherType: "IPv4", Protocol: "icmp", PortRangeMin: &echo}

	// https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/db/securitygroups_db.py#L890-L913
	created, err := service.CreateSecurityGroupRule(projectID, database.ID, fromWeb)
	require.NoError(t, err)
	icmp, err := service.CreateSecurityGroupRule(projectID, database.ID, ping)
	require.NoError(t, err)

	assert.Equal(t, web.ID, created.Rule.RemoteGroupID)
	assert.False(t, created.Rule.RemoteIPPrefix.IsValid())
	assert.Equal(t, ping.PortRangeMin, icmp.Rule.PortRangeMin)
	assert.Nil(t, icmp.Rule.PortRangeMax)
	for _, remoteGroupID := range []string{"missing", foreign.ID} {
		rule := fromWeb
		rule.RemoteGroupID = remoteGroupID
		_, err := service.CreateSecurityGroupRule(projectID, database.ID, rule)
		var missing *app.SecurityGroupNotFoundError
		require.ErrorAs(t, err, &missing)
		assert.Equal(t, remoteGroupID, missing.ID)
	}

	// https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/db/securitygroups_db.py#L965-L1014
	byNumber := fromWeb
	byNumber.Protocol = "6"
	_, err = service.CreateSecurityGroupRule(projectID, database.ID, byNumber)
	assert.ErrorIs(t, err, app.ErrSecurityGroupRuleExists)
	ping.Protocol = "1"
	_, err = service.CreateSecurityGroupRule(projectID, database.ID, ping)
	assert.ErrorIs(t, err, app.ErrSecurityGroupRuleExists)
	anyAddress := domain.SecurityGroupRule{
		Direction: "egress", EtherType: "IPv4", RemoteIPPrefix: netip.MustParsePrefix("0.0.0.0/0"),
	}
	_, err = service.CreateSecurityGroupRule(projectID, database.ID, anyAddress)
	assert.ErrorIs(t, err, app.ErrSecurityGroupRuleExists)

	// https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/db/models/securitygroup.py#L73-L101
	require.NoError(t, service.DeleteSecurityGroup(projectID, web.ID))
	_, err = service.GetSecurityGroupRule(projectID, created.Rule.ID)
	assert.ErrorIs(t, err, app.ErrSecurityGroupRuleNotFound)
	remaining, err := service.GetSecurityGroup(projectID, database.ID)
	require.NoError(t, err)
	assert.Equal(t, append(database.Rules, icmp.Rule), remaining.Rules)
}
