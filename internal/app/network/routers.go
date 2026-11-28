package network

import (
	"errors"
	"slices"
	"strings"

	domain "github.com/JSYoo5B/sandstack/internal/domain/network"
)

var (
	ErrRouterNotFound = errors.New("router not found")
	ErrRouterInUse    = errors.New("router has attached interfaces")
)

func (s *Service) CreateRouter(projectID, name, description string, adminStateUp bool) (domain.Router, error) {
	return s.CreateRouterWithGateway(projectID, name, description, adminStateUp, RouterGatewayUpdate{})
}

func (s *Service) CreateRouterWithGateway(
	projectID, name, description string,
	adminStateUp bool,
	gateway RouterGatewayUpdate,
) (domain.Router, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	id, err := s.allocateID(nil)
	if err != nil {
		return domain.Router{}, err
	}
	router := domain.Router{
		ID:           id,
		ProjectID:    projectID,
		Name:         name,
		Description:  description,
		AdminStateUp: adminStateUp,
	}
	s.routers[id] = router
	if gateway.NetworkID != "" {
		if _, err := s.applyRouterGateway(&router, gateway); err != nil {
			delete(s.routers, id)
			return domain.Router{}, err
		}
	}
	s.routers[id] = router
	return cloneRouter(router), nil
}

func (s *Service) GetRouter(projectID, routerID string) (domain.Router, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	router, ok := s.routers[routerID]
	if !ok || router.ProjectID != projectID {
		return domain.Router{}, ErrRouterNotFound
	}
	return cloneRouter(router), nil
}

type RouterUpdate struct {
	Name         *string
	Description  *string
	AdminStateUp *bool
	// Routes replaces the entire route list when non-nil.
	Routes  []domain.RouterRoute
	Gateway *RouterGatewayUpdate
}

func (s *Service) UpdateRouter(projectID, routerID string, changes RouterUpdate) (domain.Router, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	router, ok := s.routers[routerID]
	if !ok || router.ProjectID != projectID {
		return domain.Router{}, ErrRouterNotFound
	}
	undo := func() {}
	if changes.Gateway != nil {
		var err error
		if undo, err = s.applyRouterGateway(&router, *changes.Gateway); err != nil {
			return domain.Router{}, err
		}
	}
	if changes.Routes != nil || changes.Gateway != nil {
		routes := router.Routes
		if changes.Routes != nil {
			routes = slices.Clone(changes.Routes)
		}
		if err := s.validateRouterRoutes(routerID, routes); err != nil {
			undo()
			return domain.Router{}, err
		}
		router.Routes = routes
	}
	if changes.Name != nil {
		router.Name = *changes.Name
	}
	if changes.Description != nil {
		router.Description = *changes.Description
	}
	if changes.AdminStateUp != nil {
		router.AdminStateUp = *changes.AdminStateUp
	}
	s.routers[routerID] = router
	return cloneRouter(router), nil
}

type RouterListFilter struct {
	Names       []string
	AdminStates []bool
}

func (f RouterListFilter) matches(router domain.Router) bool {
	return (len(f.Names) == 0 || slices.Contains(f.Names, router.Name)) &&
		(len(f.AdminStates) == 0 || slices.Contains(f.AdminStates, router.AdminStateUp))
}

// ListRouters returns the project's matching routers ordered by ID.
func (s *Service) ListRouters(projectID string, filter RouterListFilter) []domain.Router {
	page, _ := s.ListRoutersPage(projectID, filter, PageOptions{})
	return page.Items
}

func (s *Service) ListRoutersPage(
	projectID string,
	filter RouterListFilter,
	options PageOptions,
) (Page[domain.Router], error) {
	routers := []domain.Router{}
	var marker *domain.Router
	s.mu.RLock()
	if options.Limit > 0 && options.Marker != "" {
		router, exists := s.routers[options.Marker]
		if !exists || router.ProjectID != projectID {
			s.mu.RUnlock()
			return Page[domain.Router]{}, ErrRouterNotFound
		}
		marker = &router
	}
	for _, router := range s.routers {
		if router.ProjectID == projectID && filter.matches(router) {
			routers = append(routers, cloneRouter(router))
		}
	}
	s.mu.RUnlock()
	return paginate(routers, options, marker,
		func(a, b domain.Router) int { return strings.Compare(a.ID, b.ID) },
		func(router domain.Router) string { return router.ID },
	), nil
}

func cloneRouter(router domain.Router) domain.Router {
	router.Routes = slices.Clone(router.Routes)
	router.Gateway = cloneGateway(router.Gateway)
	return router
}

func (s *Service) DeleteRouter(projectID, routerID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	router, ok := s.routers[routerID]
	if !ok || router.ProjectID != projectID {
		return ErrRouterNotFound
	}
	for _, port := range s.ports {
		if port.DeviceOwner == domain.DeviceOwnerRouterInterface && port.DeviceID == routerID {
			return ErrRouterInUse
		}
	}
	if router.Gateway != nil {
		delete(s.ports, router.Gateway.PortID)
	}
	delete(s.routers, routerID)
	return nil
}
