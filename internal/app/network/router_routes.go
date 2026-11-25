package network

import (
	"errors"
	"net/netip"
	"slices"

	domain "github.com/JSYoo5B/sandstack/internal/domain/network"
)

var (
	ErrInvalidRouterRoutes         = errors.New("invalid router routes")
	ErrRouterInterfaceInUseByRoute = errors.New("static route uses the router interface subnet")
)

// validateRouterRoutes checks a full replacement while the write lock is held.
func (s *Service) validateRouterRoutes(routerID string, routes []domain.RouterRoute) error {
	if len(routes) == 0 {
		return nil
	}
	var attached []netip.Prefix
	interfaceIPs := make(map[netip.Addr]bool)
	for _, port := range s.ports {
		if port.DeviceOwner == domain.DeviceOwnerRouterInterface && port.DeviceID == routerID {
			attached = append(attached, s.subnets[port.SubnetID].CIDR)
			interfaceIPs[port.FixedIP] = true
		}
	}
	seen := make(map[domain.RouterRoute]bool, len(routes))
	for _, route := range routes {
		if !route.Destination.Addr().Is4() || route.Destination != route.Destination.Masked() ||
			route.Destination.Addr().IsLoopback() || !route.NextHop.Is4() ||
			interfaceIPs[route.NextHop] || seen[route] {
			return ErrInvalidRouterRoutes
		}
		if !slices.ContainsFunc(attached, func(cidr netip.Prefix) bool { return cidr.Contains(route.NextHop) }) {
			return ErrInvalidRouterRoutes
		}
		seen[route] = true
	}
	return nil
}

// routerRoutesUseSubnet runs with the write lock held before removing an interface.
func (s *Service) routerRoutesUseSubnet(routerID, subnetID string) bool {
	cidr := s.subnets[subnetID].CIDR
	return slices.ContainsFunc(s.routers[routerID].Routes, func(route domain.RouterRoute) bool {
		return cidr.Contains(route.NextHop)
	})
}
