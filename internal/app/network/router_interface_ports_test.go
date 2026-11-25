package network_test

import (
	"net/netip"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	app "github.com/JSYoo5B/sandstack/internal/app/network"
	domain "github.com/JSYoo5B/sandstack/internal/domain/network"
)

func TestExistingRouterInterfaceRetainsPortAndReleasesIPOnRemoval(t *testing.T) {
	seed, service := newPortFixture()
	subnet, err := service.CreateSubnet(
		seed.ProjectID, seed.ID, "", netip.MustParsePrefix("10.0.0.0/30"), true,
	)
	require.NoError(t, err)
	router, err := service.CreateRouter(seed.ProjectID, "", "", true)
	require.NoError(t, err)
	otherRouter, err := service.CreateRouter(seed.ProjectID, "", "", true)
	require.NoError(t, err)
	port, err := service.CreatePort(seed.ProjectID, seed.ID, "admin-down port", true, nil)
	require.NoError(t, err)
	_, err = service.RemoveRouterInterfaceByPort(seed.ProjectID, router.ID, port.ID, "")
	assert.ErrorIs(t, err, app.ErrRouterInterfacePortNotFound)

	attached, err := service.AddRouterInterfaceByPort(seed.ProjectID, router.ID, port.ID)
	require.NoError(t, err)
	expected := port
	expected.DeviceID = router.ID
	expected.DeviceOwner = domain.DeviceOwnerRouterInterface
	assert.Equal(t, expected, attached)
	assert.Equal(t, []domain.Port{expected}, service.ListPorts(seed.ProjectID, app.PortListFilter{}))
	_, err = service.AddRouterInterfaceByPort(seed.ProjectID, otherRouter.ID, port.ID)
	assert.ErrorIs(t, err, app.ErrPortInUse)
	assert.ErrorIs(t, service.DeletePort(seed.ProjectID, port.ID), app.ErrServicePortInUse)
	_, err = service.RemoveRouterInterfaceByPort(seed.ProjectID, otherRouter.ID, port.ID, "")
	assert.ErrorIs(t, err, app.ErrRouterInterfacePortNotFound)
	_, err = service.RemoveRouterInterfaceByPort(seed.ProjectID, router.ID, port.ID, "other-subnet")
	assert.ErrorIs(t, err, app.ErrRouterInterfaceSubnetMismatch)
	assert.Equal(t, []domain.Port{expected}, service.ListPorts(seed.ProjectID, app.PortListFilter{}))
	_, err = service.CreatePort(seed.ProjectID, seed.ID, "over pool capacity", false, nil)
	assert.ErrorIs(t, err, app.ErrPortIPExhausted)

	removed, err := service.RemoveRouterInterfaceByPort(seed.ProjectID, router.ID, port.ID, subnet.ID)
	require.NoError(t, err)
	assert.Equal(t, expected, removed)
	_, err = service.GetPort(seed.ProjectID, port.ID)
	assert.ErrorIs(t, err, app.ErrPortNotFound)
	replacement, err := service.CreatePort(seed.ProjectID, seed.ID, "reused address", false, nil)
	require.NoError(t, err)
	assert.Equal(t, port.FixedIP, replacement.FixedIP)

	_, err = service.AddRouterInterfaceByPort(seed.ProjectID, router.ID, replacement.ID)
	require.NoError(t, err)
	removed, err = service.RemoveRouterInterface(seed.ProjectID, router.ID, subnet.ID)
	require.NoError(t, err)
	assert.Equal(t, replacement.ID, removed.ID)
	generated, err := service.AddRouterInterface(seed.ProjectID, router.ID, subnet.ID)
	require.NoError(t, err)
	removed, err = service.RemoveRouterInterfaceByPort(seed.ProjectID, router.ID, generated.ID, "")
	require.NoError(t, err)
	assert.Equal(t, generated, removed)
}

func TestExistingRouterInterfacePreservesUnboundPortsOnRejection(t *testing.T) {
	seed, service := newPortFixture()
	subnet, err := service.CreateSubnet(
		seed.ProjectID, seed.ID, "", netip.MustParsePrefix("10.0.0.0/24"), true,
	)
	require.NoError(t, err)
	router, err := service.CreateRouter(seed.ProjectID, "", "", true)
	require.NoError(t, err)
	_, err = service.AddRouterInterface(seed.ProjectID, router.ID, subnet.ID)
	require.NoError(t, err)
	withIP, err := service.CreatePort(seed.ProjectID, seed.ID, "with IP", false, nil)
	require.NoError(t, err)
	withoutSubnet := ""
	withoutIP, err := service.CreatePort(seed.ProjectID, seed.ID, "without IP", false, &withoutSubnet)
	require.NoError(t, err)
	otherNetwork, err := service.Create(seed.ProjectID, "", false)
	require.NoError(t, err)
	_, err = service.CreateSubnet(
		seed.ProjectID, otherNetwork.ID, "", netip.MustParsePrefix("10.0.0.128/25"), true,
	)
	require.NoError(t, err)
	overlap, err := service.CreatePort(seed.ProjectID, otherNetwork.ID, "overlap", false, nil)
	require.NoError(t, err)

	_, err = service.AddRouterInterfaceByPort(seed.ProjectID, router.ID, "missing")
	assert.ErrorIs(t, err, app.ErrPortNotFound)
	_, err = service.AddRouterInterfaceByPort("foreign", router.ID, withIP.ID)
	assert.ErrorIs(t, err, app.ErrRouterNotFound)
	_, err = service.AddRouterInterfaceByPort(seed.ProjectID, router.ID, withIP.ID)
	assert.ErrorIs(t, err, app.ErrRouterSubnetAttached)
	_, err = service.AddRouterInterfaceByPort(seed.ProjectID, router.ID, withoutIP.ID)
	assert.ErrorIs(t, err, app.ErrRouterPortNoFixedIP)
	_, err = service.AddRouterInterfaceByPort(seed.ProjectID, router.ID, overlap.ID)
	assert.ErrorIs(t, err, app.ErrRouterSubnetOverlap)
	for _, port := range []domain.Port{withIP, withoutIP, overlap} {
		loaded, err := service.GetPort(seed.ProjectID, port.ID)
		require.NoError(t, err)
		assert.Equal(t, port, loaded)
	}
}
