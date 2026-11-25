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

func TestRouterRouteReplacementValidatesAndKeepsDetachedSnapshots(t *testing.T) {
	service, router, port := routerWithInterface(t)
	otherSubnet, err := service.CreateSubnet(
		router.ProjectID, port.NetworkID, app.SubnetCreate{
			CIDR:       netip.MustParsePrefix("10.0.1.0/24"),
			EnableDHCP: true,
		},
	)
	require.NoError(t, err)
	otherPort, err := service.AddRouterInterface(router.ProjectID, router.ID, otherSubnet.ID)
	require.NoError(t, err)
	destination := netip.MustParsePrefix("192.0.2.0/24")
	nextHop := netip.MustParseAddr("10.0.0.23")
	first := domain.RouterRoute{Destination: destination, NextHop: nextHop}
	second := domain.RouterRoute{Destination: destination, NextHop: netip.MustParseAddr("10.0.0.24")}
	input := []domain.RouterRoute{first, second}
	updated, err := service.UpdateRouter(router.ProjectID, router.ID, app.RouterUpdate{Routes: input})
	require.NoError(t, err)
	expected := []domain.RouterRoute{first, second}
	require.Equal(t, expected, updated.Routes)

	input[0] = domain.RouterRoute{}
	updated.Routes[1] = domain.RouterRoute{}
	loaded, err := service.GetRouter(router.ProjectID, router.ID)
	require.NoError(t, err)
	require.Equal(t, expected, loaded.Routes)
	loaded.Routes[0] = domain.RouterRoute{}
	listed := service.ListRouters(router.ProjectID, app.RouterListFilter{})
	require.Len(t, listed, 1)
	require.Equal(t, expected, listed[0].Routes)
	listed[0].Routes[1] = domain.RouterRoute{}

	name := "rejected name"
	for _, invalid := range [][]domain.RouterRoute{
		{{Destination: netip.MustParsePrefix("192.0.2.3/24"), NextHop: nextHop}},
		{{Destination: netip.MustParsePrefix("127.0.0.0/8"), NextHop: nextHop}},
		{{Destination: netip.MustParsePrefix("2001:db8::/64"), NextHop: nextHop}},
		{{Destination: destination, NextHop: netip.MustParseAddr("2001:db8::1")}},
		{{Destination: destination, NextHop: port.FixedIP}},
		{{Destination: destination, NextHop: otherPort.FixedIP}},
		{{Destination: destination, NextHop: netip.MustParseAddr("10.0.2.23")}},
		{first, first},
	} {
		_, err = service.UpdateRouter(router.ProjectID, router.ID, app.RouterUpdate{
			Name: &name, Routes: invalid,
		})
		assert.ErrorIs(t, err, app.ErrInvalidRouterRoutes)
	}
	loaded, err = service.GetRouter(router.ProjectID, router.ID)
	require.NoError(t, err)
	assert.Equal(t, router.Name, loaded.Name)
	assert.Equal(t, expected, loaded.Routes)

	name = "metadata only"
	updated, err = service.UpdateRouter(router.ProjectID, router.ID, app.RouterUpdate{Name: &name})
	require.NoError(t, err)
	assert.Equal(t, name, updated.Name)
	assert.Equal(t, expected, updated.Routes)
	_, err = service.RemoveRouterInterface(router.ProjectID, router.ID, port.SubnetID)
	assert.ErrorIs(t, err, app.ErrRouterInterfaceInUseByRoute)
	_, err = service.RemoveRouterInterfaceByPort(router.ProjectID, router.ID, port.ID, "")
	assert.ErrorIs(t, err, app.ErrRouterInterfaceInUseByRoute)
	_, err = service.RemoveRouterInterfaceByPort(router.ProjectID, router.ID, otherPort.ID, "")
	assert.NoError(t, err)

	updated, err = service.UpdateRouter(router.ProjectID, router.ID, app.RouterUpdate{
		Routes: []domain.RouterRoute{},
	})
	require.NoError(t, err)
	assert.Empty(t, updated.Routes)
	_, err = service.RemoveRouterInterfaceByPort(router.ProjectID, router.ID, port.ID, "")
	assert.NoError(t, err)
}

func TestConcurrentRouteReplacementAndDetachKeepRoutesReachable(t *testing.T) {
	for _, byPort := range []bool{false, true} {
		for range 8 {
			service, router, port := routerWithInterface(t)
			route := domain.RouterRoute{
				Destination: netip.MustParsePrefix("0.0.0.0/0"),
				NextHop:     netip.MustParseAddr("10.0.0.23"),
			}
			var updateErr, detachErr error
			var wait sync.WaitGroup
			release := make(chan struct{})
			wait.Go(func() {
				<-release
				_, updateErr = service.UpdateRouter(router.ProjectID, router.ID, app.RouterUpdate{
					Routes: []domain.RouterRoute{route},
				})
			})
			wait.Go(func() {
				<-release
				if byPort {
					_, detachErr = service.RemoveRouterInterfaceByPort(router.ProjectID, router.ID, port.ID, "")
					return
				}
				_, detachErr = service.RemoveRouterInterface(router.ProjectID, router.ID, port.SubnetID)
			})
			close(release)
			wait.Wait()

			loaded, err := service.GetRouter(router.ProjectID, router.ID)
			require.NoError(t, err)
			loadedPort, portErr := service.GetPort(router.ProjectID, port.ID)
			if updateErr == nil {
				assert.ErrorIs(t, detachErr, app.ErrRouterInterfaceInUseByRoute)
				assert.Equal(t, []domain.RouterRoute{route}, loaded.Routes)
				assert.NoError(t, portErr)
				assert.Equal(t, port, loadedPort)
			} else {
				assert.ErrorIs(t, updateErr, app.ErrInvalidRouterRoutes)
				assert.NoError(t, detachErr)
				assert.Empty(t, loaded.Routes)
				assert.ErrorIs(t, portErr, app.ErrPortNotFound)
			}
		}
	}
}

func routerWithInterface(t *testing.T) (*app.Service, domain.Router, domain.Port) {
	t.Helper()
	seed, service := newPortFixture()
	subnet, err := service.CreateSubnet(
		seed.ProjectID, seed.ID, app.SubnetCreate{
			CIDR:       netip.MustParsePrefix("10.0.0.0/24"),
			EnableDHCP: true,
		},
	)
	require.NoError(t, err)
	router, err := service.CreateRouter(seed.ProjectID, "existing router", "", true)
	require.NoError(t, err)
	port, err := service.AddRouterInterface(seed.ProjectID, router.ID, subnet.ID)
	require.NoError(t, err)
	return service, router, port
}
