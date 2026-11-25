package network_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	app "github.com/JSYoo5B/sandstack/internal/app/network"
	domain "github.com/JSYoo5B/sandstack/internal/domain/network"
)

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
