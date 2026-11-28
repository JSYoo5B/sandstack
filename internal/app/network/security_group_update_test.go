package network_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	app "github.com/JSYoo5B/sandstack/internal/app/network"
	domain "github.com/JSYoo5B/sandstack/internal/domain/network"
)

func TestSecurityGroupUpdateProtectsDefaultNameAndBoundMode(t *testing.T) {
	seed := domain.Network{ID: fixtureID(0), ProjectID: "owner"}
	sequence := 0
	service := app.New(seed, func() string {
		sequence++
		return fixtureID(sequence)
	})
	projectID := seed.ProjectID
	defaults := service.ListSecurityGroups(projectID, app.SecurityGroupFilter{})
	require.Len(t, defaults, 1)
	custom, err := service.CreateSecurityGroup(projectID, "custom", "before", true)
	require.NoError(t, err)
	groupIDs := []string{custom.ID}
	_, err = service.CreatePortWithOptions(projectID, seed.ID, "bound", false, nil, nil, &groupIDs)
	require.NoError(t, err)
	name, description, stateful, stateless := "renamed", "after", true, false
	reserved := "DEFAULT"

	updated, err := service.UpdateSecurityGroup(projectID, custom.ID, app.SecurityGroupUpdate{
		Name: &name, Description: &description, Stateful: &stateful,
	})
	_, reservedErr := service.UpdateSecurityGroup(projectID, custom.ID, app.SecurityGroupUpdate{Name: &reserved})
	_, boundErr := service.UpdateSecurityGroup(projectID, custom.ID, app.SecurityGroupUpdate{
		Name: &reserved, Stateful: &stateless,
	})
	_, modeErr := service.UpdateSecurityGroup(projectID, custom.ID, app.SecurityGroupUpdate{
		Description: &name, Stateful: &stateless,
	})
	_, defaultNameErr := service.UpdateSecurityGroup(projectID, defaults[0].ID, app.SecurityGroupUpdate{Name: &name})
	_, foreignErr := service.UpdateSecurityGroup("foreign", custom.ID, app.SecurityGroupUpdate{Name: &name})
	defaultDescription, err := service.UpdateSecurityGroup(projectID, defaults[0].ID, app.SecurityGroupUpdate{
		Description: &description,
	})
	require.NoError(t, err)

	assert.Equal(t, name, updated.Name)
	assert.Equal(t, description, updated.Description)
	assert.True(t, updated.Stateful)
	assert.Equal(t, custom.Rules, updated.Rules)
	assert.ErrorIs(t, reservedErr, app.ErrSecurityGroupDefaultName)
	assert.ErrorIs(t, boundErr, app.ErrSecurityGroupDefaultName)
	assert.ErrorIs(t, modeErr, app.ErrSecurityGroupInUse)
	assert.ErrorIs(t, defaultNameErr, app.ErrSecurityGroupDefaultUpdate)
	assert.ErrorIs(t, foreignErr, app.ErrSecurityGroupNotFound)
	assert.Equal(t, "default", defaultDescription.Name)
	assert.Equal(t, description, defaultDescription.Description)
	current, err := service.GetSecurityGroup(projectID, custom.ID)
	require.NoError(t, err)
	assert.Equal(t, updated, current)
	service.Reset()
	assert.Equal(t, defaults, service.ListSecurityGroups(projectID, app.SecurityGroupFilter{}))
}

func TestUnboundSecurityGroupChangesStatefulMode(t *testing.T) {
	service := app.New(domain.Network{ID: "network", ProjectID: "owner"}, nil)
	defaults := service.ListSecurityGroups("owner", app.SecurityGroupFilter{})
	require.Len(t, defaults, 1)
	stateless := false

	updated, err := service.UpdateSecurityGroup("owner", defaults[0].ID, app.SecurityGroupUpdate{Stateful: &stateless})
	require.NoError(t, err)

	assert.False(t, updated.Stateful)
	assert.Equal(t, defaults[0].Name, updated.Name)
	assert.Equal(t, defaults[0].Rules, updated.Rules)
}
