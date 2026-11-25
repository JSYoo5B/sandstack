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

func TestRouterRouteActionsOperateOnExactPairsAndValidateAtomically(t *testing.T) {
	service, router, _ := routerWithInterface(t)
	first := domain.RouterRoute{
		Destination: netip.MustParsePrefix("192.0.2.0/24"), NextHop: netip.MustParseAddr("10.0.0.21"),
	}
	second := domain.RouterRoute{Destination: first.Destination, NextHop: netip.MustParseAddr("10.0.0.22")}
	overlap := domain.RouterRoute{
		Destination: netip.MustParsePrefix("192.0.2.0/25"), NextHop: netip.MustParseAddr("10.0.0.23"),
	}
	_, err := service.UpdateRouter(router.ProjectID, router.ID, app.RouterUpdate{
		Routes: []domain.RouterRoute{first},
	})
	require.NoError(t, err)
	input := []domain.RouterRoute{first, second, overlap, second}

	added, err := service.AddRouterRoutes(router.ProjectID, router.ID, input)
	require.NoError(t, err)
	want := router
	want.Routes = []domain.RouterRoute{first, second, overlap}
	require.Equal(t, want, added)
	retried, err := service.AddRouterRoutes(router.ProjectID, router.ID, input)
	require.NoError(t, err)
	assert.Equal(t, want, retried)
	input[1] = domain.RouterRoute{}
	added.Routes[0] = domain.RouterRoute{}
	loaded, err := service.GetRouter(router.ProjectID, router.ID)
	require.NoError(t, err)
	assert.Equal(t, want, loaded)

	unreachable := domain.RouterRoute{Destination: first.Destination, NextHop: netip.MustParseAddr("10.1.0.21")}
	valid := domain.RouterRoute{Destination: first.Destination, NextHop: netip.MustParseAddr("10.0.0.24")}
	_, err = service.AddRouterRoutes(router.ProjectID, router.ID, []domain.RouterRoute{valid, unreachable})
	assert.ErrorIs(t, err, app.ErrInvalidRouterRoutes)
	loaded, err = service.GetRouter(router.ProjectID, router.ID)
	require.NoError(t, err)
	assert.Equal(t, want, loaded)
	_, err = service.RemoveRouterRoutes(router.ProjectID, router.ID, []domain.RouterRoute{{}})
	assert.ErrorIs(t, err, app.ErrInvalidRouterRoutes)

	removed, err := service.RemoveRouterRoutes(router.ProjectID, router.ID, []domain.RouterRoute{
		unreachable, second, second,
	})
	require.NoError(t, err)
	want.Routes = []domain.RouterRoute{first, overlap}
	assert.Equal(t, want, removed)
	retried, err = service.RemoveRouterRoutes(router.ProjectID, router.ID, []domain.RouterRoute{second})
	require.NoError(t, err)
	assert.Equal(t, want, retried)
	unchanged, err := service.RemoveRouterRoutes(router.ProjectID, router.ID, []domain.RouterRoute{})
	require.NoError(t, err)
	assert.Equal(t, want, unchanged)
	_, err = service.AddRouterRoutes("another project", router.ID, nil)
	assert.ErrorIs(t, err, app.ErrRouterNotFound)
	_, err = service.RemoveRouterRoutes(router.ProjectID, "unknown router", nil)
	assert.ErrorIs(t, err, app.ErrRouterNotFound)
}

func TestConcurrentRouteAddsPreserveAllDistinctPairs(t *testing.T) {
	service, router, _ := routerWithInterface(t)
	first := domain.RouterRoute{
		Destination: netip.MustParsePrefix("0.0.0.0/0"), NextHop: netip.MustParseAddr("10.0.0.21"),
	}
	_, err := service.AddRouterRoutes(router.ProjectID, router.ID, []domain.RouterRoute{first})
	require.NoError(t, err)

	expected := []domain.RouterRoute{first}
	var wait sync.WaitGroup
	release := make(chan struct{})
	for i := range 20 {
		route := domain.RouterRoute{
			Destination: netip.PrefixFrom(netip.AddrFrom4([4]byte{192, 0, 2, byte(i)}), 32),
			NextHop:     first.NextHop,
		}
		expected = append(expected, route)
		wait.Go(func() {
			<-release
			_, err := service.AddRouterRoutes(router.ProjectID, router.ID, []domain.RouterRoute{route})
			assert.NoError(t, err)
		})
	}
	close(release)
	wait.Wait()

	loaded, err := service.GetRouter(router.ProjectID, router.ID)
	require.NoError(t, err)
	require.Len(t, loaded.Routes, len(expected))
	assert.Equal(t, first, loaded.Routes[0])
	assert.ElementsMatch(t, expected, loaded.Routes)
}
