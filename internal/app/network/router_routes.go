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

func (s *Service) AddRouterRoutes(projectID, routerID string, routes []domain.RouterRoute) (domain.Router, error) {
	return s.changeRouterRoutes(projectID, routerID, routes, true)
}

func (s *Service) RemoveRouterRoutes(projectID, routerID string, routes []domain.RouterRoute) (domain.Router, error) {
	return s.changeRouterRoutes(projectID, routerID, routes, false)
}

func (s *Service) changeRouterRoutes(
	projectID, routerID string,
	routes []domain.RouterRoute,
	add bool,
) (domain.Router, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	router, ok := s.routers[routerID]
	if !ok || router.ProjectID != projectID {
		return domain.Router{}, ErrRouterNotFound
	}
	if len(routes) == 0 {
		return cloneRouter(router), nil
	}

	updated := slices.Clone(router.Routes)
	if add {
		seen := make(map[domain.RouterRoute]bool, len(updated))
		for _, route := range updated {
			seen[route] = true
		}
		for _, route := range routes {
			if !seen[route] {
				updated = append(updated, route)
				seen[route] = true
			}
		}
	} else {
		remove := make(map[domain.RouterRoute]bool, len(routes))
		for _, route := range routes {
			if !validRouterRouteSyntax(route) {
				return domain.Router{}, ErrInvalidRouterRoutes
			}
			remove[route] = true
		}
		updated = slices.DeleteFunc(updated, func(route domain.RouterRoute) bool { return remove[route] })
	}
	if err := s.validateRouterRoutes(routerID, updated); err != nil {
		return domain.Router{}, err
	}
	router.Routes = updated
	s.routers[routerID] = router
	return cloneRouter(router), nil
}

// validateRouterRoutes checks a full replacement while the write lock is held.
func (s *Service) validateRouterRoutes(routerID string, routes []domain.RouterRoute) error {
	if len(routes) == 0 {
		return nil
	}
	var attached []netip.Prefix
	interfaceIPs := make(map[netip.Addr]bool)
	for _, port := range s.ports {
		subnets := s.routerPortSubnets(routerID, port)
		for _, subnet := range subnets {
			attached = append(attached, subnet.CIDR)
		}
		if len(subnets) > 0 && port.FixedIP.IsValid() {
			interfaceIPs[port.FixedIP] = true
		}
	}
	seen := make(map[domain.RouterRoute]bool, len(routes))
	for _, route := range routes {
		if !validRouterRouteSyntax(route) || interfaceIPs[route.NextHop] || seen[route] {
			return ErrInvalidRouterRoutes
		}
		if !slices.ContainsFunc(attached, func(cidr netip.Prefix) bool { return cidr.Contains(route.NextHop) }) {
			return ErrInvalidRouterRoutes
		}
		seen[route] = true
	}
	return nil
}

func validRouterRouteSyntax(route domain.RouterRoute) bool {
	return route.Destination.Addr().Is4() && route.Destination == route.Destination.Masked() &&
		!route.Destination.Addr().IsLoopback() && route.NextHop.Is4()
}

// routerRoutesUseSubnet runs with the write lock held before removing an interface.
func (s *Service) routerRoutesUseSubnet(routerID, subnetID string) bool {
	cidr := s.subnets[subnetID].CIDR
	return slices.ContainsFunc(s.routers[routerID].Routes, func(route domain.RouterRoute) bool {
		return cidr.Contains(route.NextHop)
	})
}
