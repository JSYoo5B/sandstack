package network_test

import (
	"errors"
	"net"
	"net/netip"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	app "github.com/JSYoo5B/sandstack/internal/app/network"
	domain "github.com/JSYoo5B/sandstack/internal/domain/network"
)

func TestPortAllocationExhaustionReuseAndDeletionConstraints(t *testing.T) {
	seed, service := newPortFixture()
	subnet, err := service.CreateSubnet(
		seed.ProjectID, seed.ID, "", netip.MustParsePrefix("10.0.0.0/30"), true,
	)
	require.NoError(t, err)
	first, err := service.CreatePort(seed.ProjectID, seed.ID, "worker", false, nil)
	require.NoError(t, err)
	assert.Equal(t, subnet.ID, first.SubnetID)
	assert.Equal(t, netip.MustParseAddr("10.0.0.2"), first.FixedIP)

	_, err = service.CreatePort(seed.ProjectID, seed.ID, "another worker", false, nil)
	assert.ErrorIs(t, err, app.ErrPortIPExhausted)
	assert.ErrorIs(t, service.DeleteSubnet(seed.ProjectID, subnet.ID), app.ErrSubnetInUse)
	assert.ErrorIs(t, service.Delete(seed.ProjectID, seed.ID), app.ErrNetworkInUse)

	noSubnet := ""
	noIP, err := service.CreatePort(seed.ProjectID, seed.ID, "without IP", true, &noSubnet)
	require.NoError(t, err)
	assert.Empty(t, noIP.SubnetID)
	assert.False(t, noIP.FixedIP.IsValid())
	assert.True(t, noIP.AdminDown)

	require.NoError(t, service.DeletePort(seed.ProjectID, first.ID))
	_, err = service.GetPort(seed.ProjectID, first.ID)
	assert.ErrorIs(t, err, app.ErrPortNotFound)
	reused, err := service.CreatePort(seed.ProjectID, seed.ID, "replacement", false, &subnet.ID)
	require.NoError(t, err)
	assert.Equal(t, first.FixedIP, reused.FixedIP)
	assert.NotEqual(t, noIP.MACAddress, reused.MACAddress)
	mac, err := net.ParseMAC(reused.MACAddress)
	require.NoError(t, err)
	assert.Equal(t, byte(0b10), mac[0]&0b11)

	require.NoError(t, service.DeletePort(seed.ProjectID, reused.ID))
	require.NoError(t, service.DeleteSubnet(seed.ProjectID, subnet.ID))
	assert.ErrorIs(t, service.Delete(seed.ProjectID, seed.ID), app.ErrNetworkInUse)
	require.NoError(t, service.DeletePort(seed.ProjectID, noIP.ID))
	require.NoError(t, service.Delete(seed.ProjectID, seed.ID))
}

func TestPortSubnetSelectionAndProjectScope(t *testing.T) {
	seed, service := newPortFixture()
	withoutIP, err := service.CreatePort(seed.ProjectID, seed.ID, "no subnets", false, nil)
	require.NoError(t, err)
	assert.False(t, withoutIP.FixedIP.IsValid())
	first, err := service.CreateSubnet(
		seed.ProjectID, seed.ID, "", netip.MustParsePrefix("10.0.0.0/24"), true,
	)
	require.NoError(t, err)
	_, err = service.CreateSubnet(
		seed.ProjectID, seed.ID, "", netip.MustParsePrefix("10.0.1.0/24"), true,
	)
	require.NoError(t, err)

	_, err = service.CreatePort(seed.ProjectID, seed.ID, "ambiguous", false, nil)
	assert.ErrorIs(t, err, app.ErrPortSubnetRequired)
	withIP, err := service.CreatePort(seed.ProjectID, seed.ID, "selected", false, &first.ID)
	require.NoError(t, err)
	assert.Equal(t, first.ID, withIP.SubnetID)
	assert.Equal(t, first.AllocationStart, withIP.FixedIP)

	other, err := service.Create("other-project", "private", false)
	require.NoError(t, err)
	foreignSubnet, err := service.CreateSubnet(
		other.ProjectID, other.ID, "", netip.MustParsePrefix("10.0.0.0/24"), true,
	)
	require.NoError(t, err)
	foreignPort, err := service.CreatePort(other.ProjectID, other.ID, "", false, nil)
	require.NoError(t, err)

	_, err = service.CreatePort(seed.ProjectID, seed.ID, "", false, &foreignSubnet.ID)
	assert.ErrorIs(t, err, app.ErrPortInvalidSubnet)
	_, err = service.CreatePort(seed.ProjectID, other.ID, "", false, nil)
	assert.ErrorIs(t, err, app.ErrNetworkNotFound)
	_, err = service.GetPort(seed.ProjectID, foreignPort.ID)
	assert.ErrorIs(t, err, app.ErrPortNotFound)
	assert.ErrorIs(t, service.DeletePort(seed.ProjectID, foreignPort.ID), app.ErrPortNotFound)
	assert.ErrorIs(t, service.Delete(seed.ProjectID, other.ID), app.ErrNetworkNotFound)

	assert.Equal(t, []domain.Port{withoutIP, withIP}, service.ListPorts(seed.ProjectID, nil))
	assert.Empty(t, service.ListPorts(seed.ProjectID, []string{other.ID}))
	assert.Equal(t, []domain.Port{foreignPort}, service.ListPorts(other.ProjectID, []string{seed.ID, other.ID}))

	service.Reset()
	assert.Empty(t, service.ListPorts(seed.ProjectID, nil))
	assert.Empty(t, service.ListPorts(other.ProjectID, nil))
	assert.Empty(t, service.ListSubnets(seed.ProjectID))
	assert.Equal(t, []domain.Network{seed}, service.List(seed.ProjectID, app.ListFilter{}))
}

func TestConcurrentPortAllocationsNeverDuplicateFixedIPs(t *testing.T) {
	seed, service := newPortFixture()
	_, err := service.CreateSubnet(
		seed.ProjectID, seed.ID, "", netip.MustParsePrefix("10.0.0.0/29"), true,
	)
	require.NoError(t, err)

	const attempts = 20
	errs := make(chan error, attempts)
	var wait sync.WaitGroup
	for range attempts {
		wait.Go(func() {
			_, err := service.CreatePort(seed.ProjectID, seed.ID, "", false, nil)
			errs <- err
		})
	}
	wait.Wait()
	close(errs)

	exhausted := 0
	for err := range errs {
		if errors.Is(err, app.ErrPortIPExhausted) {
			exhausted++
		} else {
			assert.NoError(t, err)
		}
	}
	ports := service.ListPorts(seed.ProjectID, nil)
	require.Len(t, ports, 5)
	assert.Equal(t, attempts-len(ports), exhausted)
	for i, port := range ports {
		assert.Equal(t, netip.AddrFrom4([4]byte{10, 0, 0, byte(i + 2)}), port.FixedIP)
	}
}

func newPortFixture() (domain.Network, *app.Service) {
	seed := domain.Network{ID: fixtureID(1), ProjectID: "owner"}
	id := 1
	return seed, app.New(seed, func() string {
		id++
		return fixtureID(id)
	})
}
