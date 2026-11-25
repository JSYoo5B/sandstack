package network_test

import (
	"net/netip"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	app "github.com/JSYoo5B/sandstack/internal/app/network"
	domain "github.com/JSYoo5B/sandstack/internal/domain/network"
)

func TestRouterInterfaceOwnsOnlyItsManagedGatewayPort(t *testing.T) {
	seed, service := newPortFixture()
	subnet, err := service.CreateSubnet(
		seed.ProjectID, seed.ID, "", netip.MustParsePrefix("10.0.0.0/30"), true,
	)
	require.NoError(t, err)
	router, err := service.CreateRouter(seed.ProjectID, "", "", false)
	require.NoError(t, err)
	ordinary, err := service.CreatePort(seed.ProjectID, seed.ID, "ordinary", false, nil)
	require.NoError(t, err)

	managed, err := service.AddRouterInterface(seed.ProjectID, router.ID, subnet.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.Port{
		ID:          managed.ID,
		NetworkID:   seed.ID,
		ProjectID:   seed.ProjectID,
		MACAddress:  managed.MACAddress,
		DeviceID:    router.ID,
		DeviceOwner: domain.DeviceOwnerRouterInterface,
		SubnetID:    subnet.ID,
		FixedIP:     subnet.Gateway,
	}, managed)
	assert.NotEmpty(t, managed.MACAddress)
	assert.NotEqual(t, ordinary.MACAddress, managed.MACAddress)
	assert.Equal(t, subnet.AllocationStart, ordinary.FixedIP)
	assert.Equal(t, []domain.Port{ordinary, managed}, service.ListPorts(seed.ProjectID, []string{seed.ID}))
	assert.ErrorIs(t, service.DeletePort(seed.ProjectID, managed.ID), app.ErrServicePortInUse)
	assert.ErrorIs(t, service.DeleteRouter(seed.ProjectID, router.ID), app.ErrRouterInUse)

	removed, err := service.RemoveRouterInterface(seed.ProjectID, router.ID, subnet.ID)
	require.NoError(t, err)
	assert.Equal(t, managed, removed)
	_, err = service.RemoveRouterInterface(seed.ProjectID, router.ID, subnet.ID)
	assert.ErrorIs(t, err, app.ErrRouterInterfaceNotFound)
	_, err = service.GetPort(seed.ProjectID, managed.ID)
	assert.ErrorIs(t, err, app.ErrPortNotFound)
	assert.Equal(t, []domain.Port{ordinary}, service.ListPorts(seed.ProjectID, []string{seed.ID}))

	_, err = service.AddRouterInterface(seed.ProjectID, router.ID, subnet.ID)
	require.NoError(t, err)
	require.NoError(t, service.DeletePort(seed.ProjectID, ordinary.ID))
	assert.ErrorIs(t, service.DeleteSubnet(seed.ProjectID, subnet.ID), app.ErrSubnetInUse)
	assert.ErrorIs(t, service.Delete(seed.ProjectID, seed.ID), app.ErrNetworkInUse)

	service.Reset()
	assert.Empty(t, service.ListPorts(seed.ProjectID, nil))
	assert.Empty(t, service.ListRouters(seed.ProjectID))
	assert.Empty(t, service.ListSubnets(seed.ProjectID))
}

func TestRouterInterfaceRejectsWrongScopeAndConflictingSubnets(t *testing.T) {
	seed, service := newPortFixture()
	prefix := netip.MustParsePrefix("10.0.0.0/24")
	subnet, err := service.CreateSubnet(seed.ProjectID, seed.ID, "", prefix, true)
	require.NoError(t, err)
	router, err := service.CreateRouter(seed.ProjectID, "", "", true)
	require.NoError(t, err)
	otherRouter, err := service.CreateRouter(seed.ProjectID, "", "", true)
	require.NoError(t, err)
	otherNetwork, err := service.Create(seed.ProjectID, "", false)
	require.NoError(t, err)
	overlap, err := service.CreateSubnet(
		seed.ProjectID, otherNetwork.ID, "", netip.MustParsePrefix("10.0.0.128/25"), true,
	)
	require.NoError(t, err)
	foreignNetwork, err := service.Create("foreign", "", false)
	require.NoError(t, err)
	foreignSubnet, err := service.CreateSubnet(foreignNetwork.ProjectID, foreignNetwork.ID, "", prefix, true)
	require.NoError(t, err)

	_, err = service.AddRouterInterface(foreignNetwork.ProjectID, router.ID, subnet.ID)
	assert.ErrorIs(t, err, app.ErrRouterNotFound)
	_, err = service.AddRouterInterface(seed.ProjectID, router.ID, foreignSubnet.ID)
	assert.ErrorIs(t, err, app.ErrSubnetNotFound)
	_, err = service.RemoveRouterInterface(seed.ProjectID, router.ID, foreignSubnet.ID)
	assert.ErrorIs(t, err, app.ErrSubnetNotFound)
	attached, err := service.AddRouterInterface(seed.ProjectID, router.ID, subnet.ID)
	require.NoError(t, err)
	_, err = service.AddRouterInterface(seed.ProjectID, router.ID, subnet.ID)
	assert.ErrorIs(t, err, app.ErrRouterSubnetAttached)
	_, err = service.AddRouterInterface(seed.ProjectID, router.ID, overlap.ID)
	assert.ErrorIs(t, err, app.ErrRouterSubnetOverlap)
	_, err = service.AddRouterInterface(seed.ProjectID, otherRouter.ID, subnet.ID)
	assert.ErrorIs(t, err, app.ErrRouterGatewayInUse)
	_, err = service.RemoveRouterInterface(seed.ProjectID, otherRouter.ID, subnet.ID)
	assert.ErrorIs(t, err, app.ErrRouterInterfaceNotFound)
	assert.Equal(t, []domain.Port{attached}, service.ListPorts(seed.ProjectID, nil))

	_, err = service.RemoveRouterInterface(seed.ProjectID, router.ID, subnet.ID)
	require.NoError(t, err)
	require.NoError(t, service.DeleteRouter(seed.ProjectID, router.ID))
	_, err = service.AddRouterInterface(seed.ProjectID, otherRouter.ID, subnet.ID)
	require.NoError(t, err)
}

func TestConcurrentRouterInterfacesAllocateGatewayOnlyOnce(t *testing.T) {
	seed, service := newPortFixture()
	subnet, err := service.CreateSubnet(
		seed.ProjectID, seed.ID, "", netip.MustParsePrefix("10.0.0.0/24"), true,
	)
	require.NoError(t, err)
	first, err := service.CreateRouter(seed.ProjectID, "", "", true)
	require.NoError(t, err)
	second, err := service.CreateRouter(seed.ProjectID, "", "", true)
	require.NoError(t, err)

	var firstErr, secondErr error
	var wait sync.WaitGroup
	wait.Go(func() { _, firstErr = service.AddRouterInterface(seed.ProjectID, first.ID, subnet.ID) })
	wait.Go(func() { _, secondErr = service.AddRouterInterface(seed.ProjectID, second.ID, subnet.ID) })
	wait.Wait()

	if firstErr == nil {
		assert.ErrorIs(t, secondErr, app.ErrRouterGatewayInUse)
	} else {
		assert.ErrorIs(t, firstErr, app.ErrRouterGatewayInUse)
		assert.NoError(t, secondErr)
	}
	assert.Len(t, service.ListPorts(seed.ProjectID, nil), 1)
}
