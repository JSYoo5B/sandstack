package network_test

import (
	"net/netip"
	"sync"
	"testing"

	app "github.com/JSYoo5B/sandstack/internal/app/network"
	domain "github.com/JSYoo5B/sandstack/internal/domain/network"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExplicitPortAddressInfersSubnetAndSharesAllocationWithAutomaticPorts(t *testing.T) {
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
	address := second.AllocationStart

	fixed, err := service.CreatePortWithFixedIP(seed.ProjectID, seed.ID, "fixed", false, nil, address)
	require.NoError(t, err)
	assert.Equal(t, second.ID, fixed.SubnetID)
	assert.Equal(t, address, fixed.FixedIP)
	_, err = service.CreatePortWithFixedIP(seed.ProjectID, seed.ID, "duplicate", false, &second.ID, address)
	assert.ErrorIs(t, err, app.ErrPortIPAlreadyAllocated)
	_, err = service.CreatePortWithFixedIP(seed.ProjectID, seed.ID, "different subnet", false, &first.ID, address)
	assert.ErrorIs(t, err, app.ErrPortInvalidIPForSubnet)
	missing := "missing subnet"
	_, err = service.CreatePortWithFixedIP(seed.ProjectID, seed.ID, "unknown", false, &missing, address)
	assert.ErrorIs(t, err, app.ErrPortInvalidSubnet)
	_, err = service.CreatePortWithFixedIP("foreign", seed.ID, "", false, nil, address)
	assert.ErrorIs(t, err, app.ErrNetworkNotFound)
	for _, invalid := range []string{"10.0.3.0", "10.0.3.7", "10.0.4.2", "2001:db8::1"} {
		addr := netip.MustParseAddr(invalid)
		_, err = service.CreatePortWithFixedIP(seed.ProjectID, seed.ID, "", false, nil, addr)
		assert.ErrorIs(t, err, app.ErrPortInvalidIPForNetwork)
		_, err = service.CreatePortWithFixedIP(seed.ProjectID, seed.ID, "", false, &second.ID, addr)
		assert.ErrorIs(t, err, app.ErrPortInvalidIPForSubnet)
	}

	automatic, err := service.CreatePort(seed.ProjectID, seed.ID, "automatic", false, &second.ID)
	require.NoError(t, err)
	assert.Equal(t, address.Next(), automatic.FixedIP)
	assert.Equal(t, fixtureID(5), automatic.ID)

	other, err := service.Create(seed.ProjectID, "other network", false)
	require.NoError(t, err)
	otherSubnet, err := service.CreateSubnet(seed.ProjectID, other.ID, app.SubnetCreate{
		CIDR:       second.CIDR,
		EnableDHCP: true,
	})
	require.NoError(t, err)
	_, err = service.CreatePortWithFixedIP(seed.ProjectID, seed.ID, "", false, &otherSubnet.ID, address)
	assert.ErrorIs(t, err, app.ErrPortInvalidSubnet)
	otherPort, err := service.CreatePortWithFixedIP(seed.ProjectID, other.ID, "", false, nil, address)
	require.NoError(t, err)
	assert.Equal(t, otherSubnet.ID, otherPort.SubnetID)
	assert.Equal(t, fixed.FixedIP, otherPort.FixedIP)
}

func TestExplicitGatewayReservationIntegratesWithRouterInterfacesAndRoutes(t *testing.T) {
	seed, service := newPortFixture()
	subnet, err := service.CreateSubnet(
		seed.ProjectID, seed.ID, app.SubnetCreate{
			CIDR:       netip.MustParsePrefix("10.0.0.0/30"),
			EnableDHCP: true,
		},
	)
	require.NoError(t, err)
	router, err := service.CreateRouter(seed.ProjectID, "", "", true)
	require.NoError(t, err)
	fixed, err := service.CreatePortWithFixedIP(seed.ProjectID, seed.ID, "gateway", false, &subnet.ID, subnet.Gateway)
	require.NoError(t, err)
	_, err = service.AddRouterInterface(seed.ProjectID, router.ID, subnet.ID)
	assert.ErrorIs(t, err, app.ErrRouterGatewayInUse)
	attached, err := service.AddRouterInterfaceByPort(seed.ProjectID, router.ID, fixed.ID)
	require.NoError(t, err)
	assert.Equal(t, subnet.Gateway, attached.FixedIP)

	route := domain.RouterRoute{Destination: netip.MustParsePrefix("192.0.2.0/24"), NextHop: subnet.Gateway}
	_, err = service.AddRouterRoutes(seed.ProjectID, router.ID, []domain.RouterRoute{route})
	assert.ErrorIs(t, err, app.ErrInvalidRouterRoutes)
	_, err = service.RemoveRouterInterfaceByPort(seed.ProjectID, router.ID, attached.ID, "")
	require.NoError(t, err)
	automatic, err := service.CreatePort(seed.ProjectID, seed.ID, "automatic", false, nil)
	require.NoError(t, err)
	assert.Equal(t, subnet.AllocationStart, automatic.FixedIP)
	managed, err := service.AddRouterInterface(seed.ProjectID, router.ID, subnet.ID)
	require.NoError(t, err)
	assert.Equal(t, subnet.Gateway, managed.FixedIP)
}

func TestConcurrentExplicitPortAllocationsCannotShareAnAddress(t *testing.T) {
	seed, service := newPortFixture()
	subnet, err := service.CreateSubnet(
		seed.ProjectID, seed.ID, app.SubnetCreate{
			CIDR:       netip.MustParsePrefix("10.0.0.0/30"),
			EnableDHCP: true,
		},
	)
	require.NoError(t, err)
	var firstErr, secondErr error
	var wait sync.WaitGroup
	release := make(chan struct{})
	wait.Go(func() {
		<-release
		_, firstErr = service.CreatePortWithFixedIP(seed.ProjectID, seed.ID, "", false, nil, subnet.AllocationStart)
	})
	wait.Go(func() {
		<-release
		_, secondErr = service.CreatePortWithFixedIP(seed.ProjectID, seed.ID, "", false, nil, subnet.AllocationStart)
	})
	close(release)
	wait.Wait()

	if firstErr == nil {
		assert.ErrorIs(t, secondErr, app.ErrPortIPAlreadyAllocated)
	} else {
		assert.ErrorIs(t, firstErr, app.ErrPortIPAlreadyAllocated)
		assert.NoError(t, secondErr)
	}
	ports := service.ListPorts(seed.ProjectID, app.PortListFilter{})
	require.Len(t, ports, 1)
	assert.Equal(t, subnet.AllocationStart, ports[0].FixedIP)
}
