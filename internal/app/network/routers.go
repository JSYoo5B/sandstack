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
	return router, nil
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
	Routes []domain.RouterRoute
}

func (s *Service) UpdateRouter(projectID, routerID string, changes RouterUpdate) (domain.Router, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	router, ok := s.routers[routerID]
	if !ok || router.ProjectID != projectID {
		return domain.Router{}, ErrRouterNotFound
	}
	if changes.Routes != nil {
		routes := slices.Clone(changes.Routes)
		if err := s.validateRouterRoutes(routerID, routes); err != nil {
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
	routers := []domain.Router{}
	s.mu.RLock()
	for _, router := range s.routers {
		if router.ProjectID == projectID && filter.matches(router) {
			routers = append(routers, cloneRouter(router))
		}
	}
	s.mu.RUnlock()
	slices.SortFunc(routers, func(a, b domain.Router) int { return strings.Compare(a.ID, b.ID) })
	return routers
}

func cloneRouter(router domain.Router) domain.Router {
	router.Routes = slices.Clone(router.Routes)
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
	delete(s.routers, routerID)
	return nil
}
