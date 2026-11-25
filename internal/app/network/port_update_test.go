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

func TestPortUpdatePreservesAndReplacesFixedAddressAtomically(t *testing.T) {
	seed, service := newPortFixture()
	first, err := service.CreateSubnet(
		seed.ProjectID, seed.ID, app.SubnetCreate{
			CIDR:       netip.MustParsePrefix("10.0.2.0/29"),
			EnableDHCP: true,
		},
	)
	require.NoError(t, err)
	second, err := service.CreateSubnet(
		seed.ProjectID, seed.ID, app.SubnetCreate{
			CIDR:       netip.MustParsePrefix("10.0.3.0/29"),
			EnableDHCP: true,
		},
	)
	require.NoError(t, err)
	port, err := service.CreatePortWithFixedIP(seed.ProjectID, seed.ID, "port", false, &first.ID, first.Gateway)
	require.NoError(t, err)
	occupied, err := service.CreatePort(seed.ProjectID, seed.ID, "occupied", false, &second.ID)
	require.NoError(t, err)

	name, adminUp := "changed", false
	_, err = service.UpdatePort("another-project", port.ID, app.PortUpdate{Name: &name})
	assert.ErrorIs(t, err, app.ErrPortNotFound)
	_, err = service.UpdatePort(seed.ProjectID, "unallocated port", app.PortUpdate{Name: &name})
	assert.ErrorIs(t, err, app.ErrPortNotFound)
	unchanged, err := service.UpdatePort(seed.ProjectID, port.ID, app.PortUpdate{
		FixedIPs: &app.PortFixedIPUpdate{SubnetID: &first.ID},
	})
	require.NoError(t, err)
	assert.Equal(t, port, unchanged)
	unchanged, err = service.UpdatePort(seed.ProjectID, port.ID, app.PortUpdate{
		FixedIPs: &app.PortFixedIPUpdate{Address: port.FixedIP},
	})
	require.NoError(t, err)
	assert.Equal(t, port, unchanged)

	_, err = service.UpdatePort(seed.ProjectID, port.ID, app.PortUpdate{
		Name: &name, AdminStateUp: &adminUp,
		FixedIPs: &app.PortFixedIPUpdate{SubnetID: &second.ID, Address: occupied.FixedIP},
	})
	assert.ErrorIs(t, err, app.ErrPortIPAlreadyAllocated)
	loaded, err := service.GetPort(seed.ProjectID, port.ID)
	require.NoError(t, err)
	assert.Equal(t, port, loaded)
	_, err = service.CreatePortWithFixedIP(seed.ProjectID, seed.ID, "", false, &first.ID, port.FixedIP)
	assert.ErrorIs(t, err, app.ErrPortIPAlreadyAllocated)

	moved, err := service.UpdatePort(seed.ProjectID, port.ID, app.PortUpdate{
		Name: &name, AdminStateUp: &adminUp,
		FixedIPs: &app.PortFixedIPUpdate{SubnetID: &second.ID},
	})
	require.NoError(t, err)
	expected := port
	expected.Name = name
	expected.AdminDown = true
	expected.SubnetID = second.ID
	expected.FixedIP = occupied.FixedIP.Next()
	assert.Equal(t, expected, moved)
	_, err = service.CreatePortWithFixedIP(seed.ProjectID, seed.ID, "", false, &first.ID, port.FixedIP)
	assert.NoError(t, err)

	cleared, err := service.UpdatePort(seed.ProjectID, port.ID, app.PortUpdate{FixedIPs: &app.PortFixedIPUpdate{}})
	require.NoError(t, err)
	expected.SubnetID = ""
	expected.FixedIP = netip.Addr{}
	assert.Equal(t, expected, cleared)
	_, err = service.CreatePortWithFixedIP(seed.ProjectID, seed.ID, "", false, &second.ID, moved.FixedIP)
	assert.NoError(t, err)
}

func TestManagedPortMetadataCanChangeWithoutChangingItsAddress(t *testing.T) {
	service, router, port := routerWithInterface(t)
	name, adminUp := "managed name", false
	expected := port
	expected.Name = name
	expected.AdminDown = true
	updated, err := service.UpdatePort(router.ProjectID, port.ID, app.PortUpdate{
		Name: &name, AdminStateUp: &adminUp,
	})
	require.NoError(t, err)
	assert.Equal(t, expected, updated)

	name = "prohibited rename"
	_, err = service.UpdatePort(router.ProjectID, port.ID, app.PortUpdate{
		Name: &name, FixedIPs: &app.PortFixedIPUpdate{},
	})
	assert.ErrorIs(t, err, app.ErrServicePortInUse)
	loaded, err := service.GetPort(router.ProjectID, port.ID)
	require.NoError(t, err)
	assert.Equal(t, expected, loaded)
}

func TestConcurrentPortUpdatesCannotClaimSameAddress(t *testing.T) {
	seed, service := newPortFixture()
	subnet, err := service.CreateSubnet(
		seed.ProjectID, seed.ID, app.SubnetCreate{
			CIDR:       netip.MustParsePrefix("10.0.0.0/29"),
			EnableDHCP: true,
		},
	)
	require.NoError(t, err)
	first, err := service.CreatePort(seed.ProjectID, seed.ID, "first", false, nil)
	require.NoError(t, err)
	second, err := service.CreatePort(seed.ProjectID, seed.ID, "second", false, nil)
	require.NoError(t, err)
	name := "changed"
	change := app.PortUpdate{
		Name: &name, FixedIPs: &app.PortFixedIPUpdate{Address: subnet.AllocationEnd},
	}
	var firstErr, secondErr error
	var wait sync.WaitGroup
	release := make(chan struct{})
	wait.Go(func() {
		<-release
		_, firstErr = service.UpdatePort(seed.ProjectID, first.ID, change)
	})
	wait.Go(func() {
		<-release
		_, secondErr = service.UpdatePort(seed.ProjectID, second.ID, change)
	})
	close(release)
	wait.Wait()

	var winner, loser domain.Port
	if firstErr == nil {
		assert.ErrorIs(t, secondErr, app.ErrPortIPAlreadyAllocated)
		winner, loser = first, second
	} else {
		assert.ErrorIs(t, firstErr, app.ErrPortIPAlreadyAllocated)
		require.NoError(t, secondErr)
		winner, loser = second, first
	}
	winner.Name = name
	winner.FixedIP = subnet.AllocationEnd
	loaded, err := service.GetPort(seed.ProjectID, winner.ID)
	require.NoError(t, err)
	assert.Equal(t, winner, loaded)
	loaded, err = service.GetPort(seed.ProjectID, loser.ID)
	require.NoError(t, err)
	assert.Equal(t, loser, loaded)
}
