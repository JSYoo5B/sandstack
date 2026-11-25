package network

import (
	"errors"
	"slices"
	"strings"

	domain "github.com/JSYoo5B/sandstack/internal/domain/network"
)

var ErrRouterNotFound = errors.New("router not found")

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
	return router, nil
}

// ListRouters returns the project's routers ordered by ID.
func (s *Service) ListRouters(projectID string) []domain.Router {
	routers := []domain.Router{}
	s.mu.RLock()
	for _, router := range s.routers {
		if router.ProjectID == projectID {
			routers = append(routers, router)
		}
	}
	s.mu.RUnlock()
	slices.SortFunc(routers, func(a, b domain.Router) int { return strings.Compare(a.ID, b.ID) })
	return routers
}

func (s *Service) DeleteRouter(projectID, routerID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	router, ok := s.routers[routerID]
	if !ok || router.ProjectID != projectID {
		return ErrRouterNotFound
	}
	delete(s.routers, routerID)
	return nil
}
