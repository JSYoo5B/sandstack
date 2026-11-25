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
