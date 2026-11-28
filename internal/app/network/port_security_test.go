package network_test

import (
	"net/netip"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	app "github.com/JSYoo5B/sandstack/internal/app/network"
	domain "github.com/JSYoo5B/sandstack/internal/domain/network"
)

func newPortSecurityService(t *testing.T) (*app.Service, domain.Network) {
	t.Helper()
	seed := domain.Network{ID: fixtureID(0), ProjectID: "owner"}
	sequence := 0
	service := app.New(seed, func() string {
		sequence++
		return fixtureID(sequence)
	})
	_, err := service.CreateSubnet(seed.ProjectID, seed.ID, app.SubnetCreate{
		CIDR: netip.MustParsePrefix("198.51.100.0/28"), EnableDHCP: true,
	})
	require.NoError(t, err)
	return service, seed
}

func TestPortSecurityInheritsNetworkValueAndSkipsDefaultGroup(t *testing.T) {
	service, seed := newPortSecurityService(t)
	projectID := seed.ProjectID
	custom, err := service.CreateSecurityGroup(projectID, "custom", "", true)
	require.NoError(t, err)
	insecure, err := service.CreateWithPortSecurity(projectID, "insecure", false, false)
	require.NoError(t, err)
	require.True(t, insecure.PortSecurityDisabled)
	enabled, disabled := true, false

	inherited, err := service.CreatePort(projectID, insecure.ID, "inherited", false, nil)
	require.NoError(t, err)
	defaulted, err := service.CreatePort(projectID, seed.ID, "defaulted", false, nil)
	require.NoError(t, err)
	overridden, err := service.CreatePortWithSecurity(
		projectID, insecure.ID, "overridden", false, nil, nil, nil,
		app.PortSecurity{Enabled: &enabled},
	)
	require.NoError(t, err)
	explicitOff, err := service.CreatePortWithSecurity(
		projectID, seed.ID, "explicit off", false, nil, nil, nil,
		app.PortSecurity{Enabled: &disabled},
	)
	require.NoError(t, err)
	_, groupErr := service.CreatePortWithSecurity(
		projectID, seed.ID, "groups", false, nil, nil, &[]string{custom.ID},
		app.PortSecurity{Enabled: &disabled},
	)
	_, inheritedGroupErr := service.CreatePortWithOptions(
		projectID, insecure.ID, "inherited groups", false, nil, nil, &[]string{custom.ID},
	)
	_, pairErr := service.CreatePortWithSecurity(
		projectID, seed.ID, "pairs", false, nil, nil, &[]string{},
		app.PortSecurity{
			Enabled:             &disabled,
			AllowedAddressPairs: &[]domain.AddressPair{{IPAddress: "198.51.100.200"}},
		},
	)

	assert.True(t, inherited.PortSecurityDisabled)
	assert.Empty(t, inherited.SecurityGroups)
	assert.False(t, defaulted.PortSecurityDisabled)
	assert.Len(t, defaulted.SecurityGroups, 1)
	assert.False(t, overridden.PortSecurityDisabled)
	assert.True(t, explicitOff.PortSecurityDisabled)
	assert.Empty(t, explicitOff.SecurityGroups)
	assert.ErrorIs(t, groupErr, app.ErrPortSecurityRequired)
	assert.ErrorIs(t, inheritedGroupErr, app.ErrPortSecurityRequired)
	assert.ErrorIs(t, pairErr, app.ErrAddressPairRequiresPortSecurity)
	assert.Len(t, service.ListPorts(projectID, app.PortListFilter{}), 4)

	reenabled, err := service.Update(projectID, insecure.ID, app.NetworkUpdate{PortSecurityEnabled: &enabled})
	require.NoError(t, err)
	assert.False(t, reenabled.PortSecurityDisabled)
	unchanged, err := service.GetPort(projectID, inherited.ID)
	require.NoError(t, err)
	assert.True(t, unchanged.PortSecurityDisabled)
}

func TestAllowedAddressPairsDefaultToPortMACAndRejectDuplicates(t *testing.T) {
	service, seed := newPortSecurityService(t)
	projectID := seed.ProjectID
	requested := []domain.AddressPair{
		{IPAddress: "198.51.100.200"},
		{IPAddress: "203.0.113.0/24", MACAddress: "fa:16:3e:00:00:01"},
	}

	port, err := service.CreatePortWithSecurity(
		projectID, seed.ID, "pairs", false, nil, nil, nil,
		app.PortSecurity{AllowedAddressPairs: &requested},
	)

	require.NoError(t, err)
	assert.Equal(t, []domain.AddressPair{
		{IPAddress: "198.51.100.200", MACAddress: port.MACAddress},
		{IPAddress: "203.0.113.0/24", MACAddress: "fa:16:3e:00:00:01"},
	}, port.AllowedAddressPairs)
	requested[0].IPAddress = "192.0.2.1"
	port.AllowedAddressPairs[1].IPAddress = "192.0.2.2"
	readBack, err := service.GetPort(projectID, port.ID)
	require.NoError(t, err)
	assert.Equal(t, "198.51.100.200", readBack.AllowedAddressPairs[0].IPAddress)
	assert.Equal(t, "203.0.113.0/24", readBack.AllowedAddressPairs[1].IPAddress)

	duplicate := []domain.AddressPair{
		{IPAddress: "198.51.100.201"},
		{IPAddress: "198.51.100.201", MACAddress: port.MACAddress},
	}
	_, err = service.UpdatePort(projectID, port.ID, app.PortUpdate{AllowedAddressPairs: &duplicate})
	var duplicateErr *app.DuplicateAddressPairError
	require.ErrorAs(t, err, &duplicateErr)
	assert.ErrorIs(t, err, app.ErrDuplicateAddressPair)
	assert.Equal(t, duplicate[1], duplicateErr.Pair)
	afterDuplicate, err := service.GetPort(projectID, port.ID)
	require.NoError(t, err)
	assert.Equal(t, readBack, afterDuplicate)

	ownAddress := []domain.AddressPair{{IPAddress: port.FixedIP.String()}}
	own, err := service.UpdatePort(projectID, port.ID, app.PortUpdate{AllowedAddressPairs: &ownAddress})
	require.NoError(t, err)
	assert.Equal(t, []domain.AddressPair{
		{IPAddress: port.FixedIP.String(), MACAddress: port.MACAddress},
	}, own.AllowedAddressPairs)
	cleared, err := service.UpdatePort(projectID, port.ID, app.PortUpdate{
		AllowedAddressPairs: &[]domain.AddressPair{},
	})
	require.NoError(t, err)
	assert.Empty(t, cleared.AllowedAddressPairs)
}

func TestDisablingPortSecurityRequiresNoGroupsOrPairs(t *testing.T) {
	service, seed := newPortSecurityService(t)
	projectID := seed.ProjectID
	pairs := []domain.AddressPair{{IPAddress: "198.51.100.200"}}
	port, err := service.CreatePortWithSecurity(
		projectID, seed.ID, "secured", false, nil, nil, nil,
		app.PortSecurity{AllowedAddressPairs: &pairs},
	)
	require.NoError(t, err)
	require.Len(t, port.SecurityGroups, 1)
	disabled, enabled := false, true
	newName := "must not be written"
	empty := []string{}
	noPairs := []domain.AddressPair{}

	_, pairsErr := service.UpdatePort(projectID, port.ID, app.PortUpdate{
		Name: &newName, PortSecurityEnabled: &disabled, SecurityGroups: &empty,
	})
	_, groupsErr := service.UpdatePort(projectID, port.ID, app.PortUpdate{
		Name: &newName, PortSecurityEnabled: &disabled, AllowedAddressPairs: &noPairs,
	})
	_, assignErr := service.UpdatePort(projectID, port.ID, app.PortUpdate{
		PortSecurityEnabled: &disabled, AllowedAddressPairs: &noPairs, SecurityGroups: &port.SecurityGroups,
	})
	_, addPairsErr := service.UpdatePort(projectID, port.ID, app.PortUpdate{
		PortSecurityEnabled: &disabled, AllowedAddressPairs: &pairs, SecurityGroups: &empty,
	})

	assert.ErrorIs(t, pairsErr, app.ErrAddressPairRequiresPortSecurity)
	assert.ErrorIs(t, groupsErr, app.ErrPortHasSecurityGroup)
	assert.ErrorIs(t, assignErr, app.ErrPortSecurityRequired)
	assert.ErrorIs(t, addPairsErr, app.ErrAddressPairRequiresPortSecurity)
	unchanged, err := service.GetPort(projectID, port.ID)
	require.NoError(t, err)
	assert.Equal(t, port, unchanged)

	insecure, err := service.UpdatePort(projectID, port.ID, app.PortUpdate{
		PortSecurityEnabled: &disabled, AllowedAddressPairs: &noPairs, SecurityGroups: &empty,
	})
	require.NoError(t, err)
	assert.True(t, insecure.PortSecurityDisabled)
	assert.Empty(t, insecure.SecurityGroups)
	assert.Empty(t, insecure.AllowedAddressPairs)
	renamed, err := service.UpdatePort(projectID, port.ID, app.PortUpdate{Name: &newName})
	require.NoError(t, err)
	assert.Equal(t, newName, renamed.Name)
	_, err = service.UpdatePort(projectID, port.ID, app.PortUpdate{AllowedAddressPairs: &pairs})
	assert.ErrorIs(t, err, app.ErrAddressPairRequiresPortSecurity)

	restored, err := service.UpdatePort(projectID, port.ID, app.PortUpdate{
		PortSecurityEnabled: &enabled, AllowedAddressPairs: &pairs, SecurityGroups: &port.SecurityGroups,
	})
	require.NoError(t, err)
	assert.False(t, restored.PortSecurityDisabled)
	assert.Equal(t, port.SecurityGroups, restored.SecurityGroups)
	assert.Equal(t, port.AllowedAddressPairs, restored.AllowedAddressPairs)
}
