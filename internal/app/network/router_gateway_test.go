package network_test

import (
	"net/netip"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	app "github.com/JSYoo5B/sandstack/internal/app/network"
	domain "github.com/JSYoo5B/sandstack/internal/domain/network"
)

func TestExternalNetworkIsReadableButOwnedByItsProject(t *testing.T) {
	seed, external, _, service := newExternalNetworkFixture("203.0.113.0/29")
	name := "renamed"

	visible, err := service.Get(seed.ProjectID, external.ID)
	require.NoError(t, err)
	listed := service.List(seed.ProjectID, app.ListFilter{External: []bool{true}})
	private := service.List(seed.ProjectID, app.ListFilter{External: []bool{false}})
	_, updateErr := service.Update(seed.ProjectID, external.ID, app.NetworkUpdate{Name: &name})
	deleteErr := service.Delete(seed.ProjectID, external.ID)
	_, portErr := service.CreatePort(seed.ProjectID, external.ID, "", false, nil)

	assert.True(t, visible.External)
	assert.Equal(t, external.ProjectID, visible.ProjectID)
	assert.Equal(t, []string{fixtureID(3)}, visible.SubnetIDs)
	assert.Equal(t, []domain.Network{visible}, listed)
	assert.Equal(t, []string{seed.ID}, networkIDs(private))
	assert.ErrorIs(t, updateErr, app.ErrNetworkForbidden)
	assert.ErrorIs(t, deleteErr, app.ErrNetworkForbidden)
	assert.ErrorIs(t, portErr, app.ErrNetworkNotFound)
}

func TestRouterGatewayAllocatesAndReleasesExternalAddress(t *testing.T) {
	seed, external, externalSubnet, service := newExternalNetworkFixture("203.0.113.0/29")
	gateway := app.RouterGatewayUpdate{NetworkID: external.ID}

	first, err := service.CreateRouterWithGateway(seed.ProjectID, "first", "", true, gateway)
	require.NoError(t, err)
	second, err := service.CreateRouter(seed.ProjectID, "second", "", true)
	require.NoError(t, err)
	second, err = service.UpdateRouter(seed.ProjectID, second.ID, app.RouterUpdate{Gateway: &gateway})
	require.NoError(t, err)
	unchanged, err := service.UpdateRouter(seed.ProjectID, second.ID, app.RouterUpdate{Gateway: &gateway})
	require.NoError(t, err)

	require.NotNil(t, first.Gateway)
	assert.Equal(t, domain.RouterGateway{
		NetworkID:  external.ID,
		PortID:     first.Gateway.PortID,
		SubnetID:   externalSubnet.ID,
		Address:    netip.MustParseAddr("203.0.113.2"),
		EnableSNAT: true,
	}, *first.Gateway)
	require.NotNil(t, second.Gateway)
	assert.Equal(t, netip.MustParseAddr("203.0.113.3"), second.Gateway.Address)
	assert.Equal(t, second, unchanged)
	_, err = service.GetPort(seed.ProjectID, first.Gateway.PortID)
	assert.ErrorIs(t, err, app.ErrPortNotFound)
	assert.Empty(t, service.ListPorts(seed.ProjectID, app.PortListFilter{}))

	cleared, err := service.UpdateRouter(seed.ProjectID, first.ID, app.RouterUpdate{Gateway: &app.RouterGatewayUpdate{}})
	require.NoError(t, err)
	assert.Nil(t, cleared.Gateway)
	require.NoError(t, service.DeleteRouter(seed.ProjectID, second.ID))
	third, err := service.CreateRouterWithGateway(seed.ProjectID, "third", "", true, gateway)
	require.NoError(t, err)
	require.NotNil(t, third.Gateway)
	assert.Equal(t, netip.MustParseAddr("203.0.113.2"), third.Gateway.Address)
}

func TestRouterGatewayRejectsUnusableNetworksWithoutChanges(t *testing.T) {
	seed, external, _, service := newExternalNetworkFixture("203.0.113.0/29")
	overlapping, err := service.CreateSubnet(seed.ProjectID, seed.ID, app.SubnetCreate{
		CIDR: netip.MustParsePrefix("203.0.113.0/28"),
	})
	require.NoError(t, err)
	router, err := service.CreateRouter(seed.ProjectID, "router", "", true)
	require.NoError(t, err)

	for _, tc := range []struct {
		name      string
		networkID string
		want      error
	}{
		{name: "missing network", networkID: fixtureID(99), want: app.ErrNetworkNotFound},
		{name: "private network", networkID: seed.ID, want: app.ErrNetworkNotExternal},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gateway := app.RouterGatewayUpdate{NetworkID: tc.networkID}

			_, createErr := service.CreateRouterWithGateway(seed.ProjectID, tc.name, "", true, gateway)
			_, updateErr := service.UpdateRouter(seed.ProjectID, router.ID, app.RouterUpdate{Gateway: &gateway})

			assert.ErrorIs(t, createErr, tc.want)
			assert.ErrorIs(t, updateErr, tc.want)
		})
	}
	var missing *app.NetworkNotFoundError
	_, err = service.CreateRouterWithGateway(seed.ProjectID, "", "", true, app.RouterGatewayUpdate{NetworkID: fixtureID(99)})
	require.ErrorAs(t, err, &missing)
	assert.Equal(t, fixtureID(99), missing.ID)

	_, err = service.AddRouterInterface(seed.ProjectID, router.ID, overlapping.ID)
	require.NoError(t, err)
	gateway := app.RouterGatewayUpdate{NetworkID: external.ID}
	_, err = service.UpdateRouter(seed.ProjectID, router.ID, app.RouterUpdate{Gateway: &gateway})

	assert.ErrorIs(t, err, app.ErrRouterSubnetOverlap)
	assert.Equal(t, []domain.Router{router}, service.ListRouters(seed.ProjectID, app.RouterListFilter{}))

	_, err = service.RemoveRouterInterface(seed.ProjectID, router.ID, overlapping.ID)
	require.NoError(t, err)
	_, err = service.UpdateRouter(seed.ProjectID, router.ID, app.RouterUpdate{Gateway: &gateway})
	require.NoError(t, err)
	_, err = service.AddRouterInterface(seed.ProjectID, router.ID, overlapping.ID)

	assert.ErrorIs(t, err, app.ErrRouterSubnetOverlap)
}

func TestRouterGatewayKeepsRoutesReachable(t *testing.T) {
	seed, external, _, service := newExternalNetworkFixture("203.0.113.0/29")
	gateway := app.RouterGatewayUpdate{NetworkID: external.ID}
	router, err := service.CreateRouterWithGateway(seed.ProjectID, "router", "", true, gateway)
	require.NoError(t, err)
	upstream := []domain.RouterRoute{{
		Destination: netip.MustParsePrefix("198.51.100.0/24"),
		NextHop:     netip.MustParseAddr("203.0.113.1"),
	}}
	ownAddress := []domain.RouterRoute{{
		Destination: netip.MustParsePrefix("198.51.100.0/24"),
		NextHop:     router.Gateway.Address,
	}}

	_, ownErr := service.UpdateRouter(seed.ProjectID, router.ID, app.RouterUpdate{Routes: ownAddress})
	routed, err := service.UpdateRouter(seed.ProjectID, router.ID, app.RouterUpdate{Routes: upstream})
	require.NoError(t, err)
	_, clearErr := service.UpdateRouter(seed.ProjectID, router.ID, app.RouterUpdate{Gateway: &app.RouterGatewayUpdate{}})
	current, err := service.GetRouter(seed.ProjectID, router.ID)
	require.NoError(t, err)
	replacement, err := service.CreateRouterWithGateway(seed.ProjectID, "replacement", "", true, gateway)
	require.NoError(t, err)

	assert.ErrorIs(t, ownErr, app.ErrInvalidRouterRoutes)
	assert.Equal(t, upstream, routed.Routes)
	assert.ErrorIs(t, clearErr, app.ErrInvalidRouterRoutes)
	assert.Equal(t, routed, current)
	assert.NotEqual(t, router.Gateway.Address, replacement.Gateway.Address)

	cleared, err := service.UpdateRouter(seed.ProjectID, router.ID, app.RouterUpdate{
		Routes: []domain.RouterRoute{}, Gateway: &app.RouterGatewayUpdate{},
	})
	require.NoError(t, err)
	assert.Nil(t, cleared.Gateway)
	assert.Empty(t, cleared.Routes)
}

func TestRouterGatewayExhaustionLeavesNoRouter(t *testing.T) {
	seed, external, _, service := newExternalNetworkFixture("203.0.113.0/30")
	gateway := app.RouterGatewayUpdate{NetworkID: external.ID}
	first, err := service.CreateRouterWithGateway(seed.ProjectID, "first", "", true, gateway)
	require.NoError(t, err)

	_, err = service.CreateRouterWithGateway(seed.ProjectID, "second", "", true, gateway)

	assert.ErrorIs(t, err, app.ErrPortIPExhausted)
	assert.Equal(t, []domain.Router{first}, service.ListRouters(seed.ProjectID, app.RouterListFilter{}))
}

func TestResetRestoresExternalNetworkAndClearsGateways(t *testing.T) {
	seed, external, _, service := newExternalNetworkFixture("203.0.113.0/29")
	gateway := app.RouterGatewayUpdate{NetworkID: external.ID}
	_, err := service.CreateRouterWithGateway(seed.ProjectID, "router", "", true, gateway)
	require.NoError(t, err)
	baseline := service.List(seed.ProjectID, app.ListFilter{})

	service.Reset()
	router, err := service.CreateRouterWithGateway(seed.ProjectID, "router", "", true, gateway)
	require.NoError(t, err)

	assert.Equal(t, baseline, service.List(seed.ProjectID, app.ListFilter{}))
	assert.Equal(t, netip.MustParseAddr("203.0.113.2"), router.Gateway.Address)
}

func newExternalNetworkFixture(cidr string) (domain.Network, domain.Network, domain.Subnet, *app.Service) {
	seed := domain.Network{ID: fixtureID(1), ProjectID: "owner"}
	external := domain.Network{ID: fixtureID(2), Name: "public", ProjectID: "admin"}
	prefix := netip.MustParsePrefix(cidr)
	subnet := domain.Subnet{
		ID:      fixtureID(3),
		CIDR:    prefix,
		Gateway: prefix.Addr().Next(),
		AllocationPools: []domain.AllocationPool{{
			Start: prefix.Addr().Next().Next(),
			End:   lastHost(prefix),
		}},
	}
	id := 3
	service := app.NewWithExternalNetwork(seed, external, subnet, func() string {
		id++
		return fixtureID(id)
	})
	external.External = true
	external.SubnetIDs = []string{subnet.ID}
	subnet.NetworkID = external.ID
	subnet.ProjectID = external.ProjectID
	return seed, external, subnet, service
}

func lastHost(prefix netip.Prefix) netip.Addr {
	address := prefix.Addr()
	for next := address.Next(); prefix.Contains(next); next = next.Next() {
		address = next
	}
	return address.Prev()
}

func networkIDs(networks []domain.Network) []string {
	ids := make([]string, len(networks))
	for i, network := range networks {
		ids[i] = network.ID
	}
	return ids
}
