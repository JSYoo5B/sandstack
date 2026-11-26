package network_test

import (
	"net/netip"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	app "github.com/JSYoo5B/sandstack/internal/app/network"
	domain "github.com/JSYoo5B/sandstack/internal/domain/network"
)

func TestPortSecurityGroupBindingsStayScopedAndDetached(t *testing.T) {
	seed := domain.Network{ID: fixtureID(0), ProjectID: "owner"}
	sequence := 0
	service := app.New(seed, func() string {
		sequence++
		return fixtureID(sequence)
	})
	projectID := seed.ProjectID
	groups := service.ListSecurityGroups(projectID, app.SecurityGroupFilter{})
	require.Len(t, groups, 1)
	custom, err := service.CreateSecurityGroup(projectID, "custom", "", true)
	require.NoError(t, err)
	stateless, err := service.CreateSecurityGroup(projectID, "stateless", "", false)
	require.NoError(t, err)
	foreign, err := service.CreateSecurityGroup("foreign", "foreign", "", true)
	require.NoError(t, err)
	subnet, err := service.CreateSubnet(projectID, seed.ID, app.SubnetCreate{
		CIDR: netip.MustParsePrefix("198.51.100.0/29"), EnableDHCP: true,
	})
	require.NoError(t, err)
	router, err := service.CreateRouter(projectID, "", "", true)
	require.NoError(t, err)

	defaulted, err := service.CreatePort(projectID, seed.ID, "default", false, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{groups[0].ID}, defaulted.SecurityGroups)
	empty := []string{}
	unbound, err := service.CreatePortWithOptions(projectID, seed.ID, "empty", false, nil, nil, &empty)
	require.NoError(t, err)
	assert.Empty(t, unbound.SecurityGroups)
	_, err = service.CreatePortWithOptions(projectID, seed.ID, "denied", false, nil, nil, &[]string{foreign.ID})
	var missing *app.SecurityGroupNotFoundError
	require.ErrorAs(t, err, &missing)
	assert.ErrorIs(t, err, app.ErrSecurityGroupNotFound)
	assert.Equal(t, foreign.ID, missing.ID)
	mixed := []string{custom.ID, stateless.ID}
	_, err = service.CreatePortWithOptions(projectID, seed.ID, "mixed", false, nil, nil, &mixed)
	assert.ErrorIs(t, err, app.ErrSecurityGroupConflict)
	assert.Len(t, service.ListPorts(projectID, app.PortListFilter{}), 2)

	selected := []string{custom.ID, groups[0].ID}
	port, err := service.CreatePortWithOptions(projectID, seed.ID, "explicit", false, nil, nil, &selected)
	require.NoError(t, err)
	selected[0], port.SecurityGroups[0] = foreign.ID, foreign.ID
	readBack, err := service.GetPort(projectID, port.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{custom.ID, groups[0].ID}, readBack.SecurityGroups)
	readBack.SecurityGroups[0] = foreign.ID
	list := service.ListPorts(projectID, app.PortListFilter{IDs: []string{port.ID}})
	require.Len(t, list, 1)
	assert.Equal(t, custom.ID, list[0].SecurityGroups[0])
	list[0].SecurityGroups[0] = foreign.ID
	assert.ErrorIs(t, service.DeleteSecurityGroup(projectID, custom.ID), app.ErrSecurityGroupInUse)

	attached, err := service.AddRouterInterfaceByPort(projectID, router.ID, port.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{custom.ID, groups[0].ID}, attached.SecurityGroups)
	attached.SecurityGroups[0] = foreign.ID
	updated, err := service.UpdatePort(projectID, port.ID, app.PortUpdate{SecurityGroups: &[]string{custom.ID}})
	require.NoError(t, err)
	assert.Equal(t, []string{custom.ID}, updated.SecurityGroups)
	assert.ErrorIs(t, service.DeleteSecurityGroup(projectID, custom.ID), app.ErrSecurityGroupInUse)
	_, err = service.RemoveRouterInterfaceByPort(projectID, router.ID, port.ID, subnet.ID)
	require.NoError(t, err)
	require.NoError(t, service.DeleteSecurityGroup(projectID, custom.ID))
	generated, err := service.AddRouterInterface(projectID, router.ID, subnet.ID)
	require.NoError(t, err)
	assert.Empty(t, generated.SecurityGroups)
	_, err = service.UpdatePort(projectID, generated.ID, app.PortUpdate{SecurityGroups: &[]string{foreign.ID}})
	assert.ErrorIs(t, err, app.ErrPortSecurityRequired)
	cleared, err := service.UpdatePort(projectID, generated.ID, app.PortUpdate{SecurityGroups: &empty})
	require.NoError(t, err)
	assert.Empty(t, cleared.SecurityGroups)
}

func TestUpdatePortSecurityGroupsCommitsWithOtherChanges(t *testing.T) {
	seed := domain.Network{ID: fixtureID(0), ProjectID: "owner"}
	sequence := 0
	service := app.New(seed, func() string {
		sequence++
		return fixtureID(sequence)
	})
	custom, err := service.CreateSecurityGroup(seed.ProjectID, "custom", "", true)
	require.NoError(t, err)
	_, err = service.CreateSubnet(seed.ProjectID, seed.ID, app.SubnetCreate{
		CIDR: netip.MustParsePrefix("198.51.100.0/29"), EnableDHCP: true,
	})
	require.NoError(t, err)
	original, err := service.CreatePort(seed.ProjectID, seed.ID, "original", false, nil)
	require.NoError(t, err)
	require.Len(t, original.SecurityGroups, 1)

	newName := "renamed"
	adminUp := false
	missing := []string{fixtureID(100)}
	change := app.PortUpdate{
		Name: &newName, AdminStateUp: &adminUp, FixedIPs: &app.PortFixedIPUpdate{}, SecurityGroups: &missing,
	}
	_, err = service.UpdatePort(seed.ProjectID, original.ID, change)
	assert.ErrorIs(t, err, app.ErrSecurityGroupNotFound)
	unchanged, err := service.GetPort(seed.ProjectID, original.ID)
	require.NoError(t, err)
	assert.Equal(t, original, unchanged)

	selected := []string{custom.ID}
	change.SecurityGroups = &selected
	updated, err := service.UpdatePort(seed.ProjectID, original.ID, change)
	require.NoError(t, err)
	assert.Equal(t, newName, updated.Name)
	assert.True(t, updated.AdminDown)
	assert.False(t, updated.FixedIP.IsValid())
	assert.Equal(t, selected, updated.SecurityGroups)
	selected[0], updated.SecurityGroups[0] = missing[0], missing[0]

	preserved, err := service.UpdatePort(seed.ProjectID, original.ID, app.PortUpdate{})
	require.NoError(t, err)
	assert.Equal(t, []string{custom.ID}, preserved.SecurityGroups)
	assert.ErrorIs(t, service.DeleteSecurityGroup(seed.ProjectID, custom.ID), app.ErrSecurityGroupInUse)

	empty := []string{}
	cleared, err := service.UpdatePort(seed.ProjectID, original.ID, app.PortUpdate{SecurityGroups: &empty})
	require.NoError(t, err)
	assert.Empty(t, cleared.SecurityGroups)
	require.NoError(t, service.DeleteSecurityGroup(seed.ProjectID, custom.ID))
}
