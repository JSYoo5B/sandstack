package network_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	app "github.com/JSYoo5B/sandstack/internal/app/network"
	domain "github.com/JSYoo5B/sandstack/internal/domain/network"
)

func TestDefaultSecurityGroupFiltersAndRulesStayScoped(t *testing.T) {
	const projectID = "owner"
	service := app.New(domain.Network{ID: "network", ProjectID: projectID}, nil)
	groups := service.ListSecurityGroups(projectID, app.SecurityGroupFilter{})
	require.Len(t, groups, 1)
	group := groups[0]
	require.Len(t, group.Rules, 4)
	assert.True(t, group.Stateful)
	assert.Equal(t, "default", group.Name)
	assert.Equal(t, projectID, group.ProjectID)
	filters := app.SecurityGroupFilter{
		IDs: []string{"unknown", group.ID}, Names: []string{"other", "default"},
		ProjectIDs: []string{"other", projectID}, TenantIDs: []string{projectID},
	}
	assert.Equal(t, []domain.SecurityGroup{group}, service.ListSecurityGroups(projectID, filters))
	filters.TenantIDs = []string{"other"}
	assert.Empty(t, service.ListSecurityGroups(projectID, filters))
	assert.Empty(t, service.ListSecurityGroups("foreign", app.SecurityGroupFilter{}))
	_, err := service.GetSecurityGroup("foreign", group.ID)
	assert.ErrorIs(t, err, app.ErrSecurityGroupNotFound)
	_, err = service.GetSecurityGroup(projectID, "missing")
	assert.ErrorIs(t, err, app.ErrSecurityGroupNotFound)

	detail, err := service.GetSecurityGroup(projectID, group.ID)
	require.NoError(t, err)
	assert.Equal(t, group, detail)
	detail.Rules[0].Direction = "mutated by caller"
	assert.Equal(t, []domain.SecurityGroup{group}, service.ListSecurityGroups(projectID, app.SecurityGroupFilter{}))
}

func TestCustomSecurityGroupsShareFiltersAndRemainProjectOwned(t *testing.T) {
	const projectID = "owner"
	sequence := 0
	service := app.New(domain.Network{ID: "network", ProjectID: projectID}, func() string {
		sequence++
		return fixtureID(sequence)
	})
	defaultGroups := service.ListSecurityGroups(projectID, app.SecurityGroupFilter{})
	require.Len(t, defaultGroups, 1)
	assert.ErrorIs(t, service.DeleteSecurityGroup(projectID, defaultGroups[0].ID), app.ErrSecurityGroupDefaultDelete)
	assert.ErrorIs(t, service.DeleteSecurityGroup("foreign", defaultGroups[0].ID), app.ErrSecurityGroupNotFound)
	_, err := service.CreateSecurityGroup(projectID, "dEFault", "", true)
	assert.ErrorIs(t, err, app.ErrSecurityGroupDefaultName)

	first, err := service.CreateSecurityGroup(projectID, "custom", "stateful disabled", false)
	require.NoError(t, err)
	second, err := service.CreateSecurityGroup(projectID, "custom", "", true)
	require.NoError(t, err)
	foreign, err := service.CreateSecurityGroup("foreign", "custom", "", true)
	require.NoError(t, err)
	assert.False(t, first.Stateful)
	assert.Equal(t, "stateful disabled", first.Description)
	assert.Equal(t, []domain.SecurityGroupRule{
		{ID: fixtureID(2), Direction: "egress", EtherType: "IPv4"},
		{ID: fixtureID(3), Direction: "egress", EtherType: "IPv6"},
	}, first.Rules)
	filter := app.SecurityGroupFilter{Names: []string{"custom"}}
	assert.Equal(t, []domain.SecurityGroup{first, second}, service.ListSecurityGroups(projectID, filter))
	assert.Equal(t, []domain.SecurityGroup{foreign}, service.ListSecurityGroups("foreign", filter))
	assert.ErrorIs(t, service.DeleteSecurityGroup("foreign", first.ID), app.ErrSecurityGroupNotFound)
	_, err = service.GetSecurityGroup(projectID, foreign.ID)
	assert.ErrorIs(t, err, app.ErrSecurityGroupNotFound)
	first.Rules[0].Direction = "mutated response"
	current, err := service.GetSecurityGroup(projectID, first.ID)
	require.NoError(t, err)
	assert.Equal(t, "egress", current.Rules[0].Direction)
	require.NoError(t, service.DeleteSecurityGroup(projectID, first.ID))
	assert.ErrorIs(t, service.DeleteSecurityGroup(projectID, first.ID), app.ErrSecurityGroupNotFound)
	service.Reset()
	assert.Equal(t, defaultGroups, service.ListSecurityGroups(projectID, app.SecurityGroupFilter{}))
	assert.Empty(t, service.ListSecurityGroups("foreign", app.SecurityGroupFilter{}))
}

func TestSecurityGroupCreationReservesEveryIDBeforePublishing(t *testing.T) {
	const projectID = "owner"
	seed := domain.Network{ID: "network", ProjectID: projectID}
	defaultGroups := app.New(seed, nil).ListSecurityGroups(projectID, app.SecurityGroupFilter{})
	require.Len(t, defaultGroups, 1)
	groupID, ipv4ID, ipv6ID, networkID := fixtureID(1), fixtureID(2), fixtureID(3), fixtureID(4)
	ids := []string{
		defaultGroups[0].ID, groupID, groupID, ipv4ID, "invalid",
		groupID, ipv4ID, defaultGroups[0].Rules[0].ID, ipv6ID, ipv6ID, networkID,
	}
	service := app.New(seed, func() string {
		id := ids[0]
		ids = ids[1:]
		return id
	})

	_, err := service.CreateSecurityGroup(projectID, "partial", "", true)
	assert.ErrorIs(t, err, app.ErrNetworkIDGeneration)
	assert.Equal(t, defaultGroups, service.ListSecurityGroups(projectID, app.SecurityGroupFilter{}))

	created, err := service.CreateSecurityGroup(projectID, "complete", "", true)
	require.NoError(t, err)
	assert.Equal(t, groupID, created.ID)
	require.Len(t, created.Rules, 2)
	assert.Equal(t, ipv4ID, created.Rules[0].ID)
	assert.Equal(t, ipv6ID, created.Rules[1].ID)
	createdNetwork, err := service.Create(projectID, "network", false)
	require.NoError(t, err)
	assert.Equal(t, networkID, createdNetwork.ID)
}
