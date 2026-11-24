package network

import (
	"errors"
	"sync"

	domain "github.com/JSYoo5B/sandstack/internal/domain/network"
)

var ErrNetworkNotFound = errors.New("network not found")

type Service struct {
	seed domain.Network
	mu   sync.RWMutex
	down bool
}

// New starts a service with one private network owned by seed.ProjectID.
func New(seed domain.Network) *Service {
	return &Service{seed: seed, down: seed.Down}
}

func (s *Service) Get(projectID, networkID string) (domain.Network, error) {
	if projectID != s.seed.ProjectID || networkID != s.seed.ID {
		return domain.Network{}, ErrNetworkNotFound
	}
	return s.snapshot(), nil
}

func (s *Service) List(projectID string) []domain.Network {
	if projectID != s.seed.ProjectID {
		return []domain.Network{}
	}
	return []domain.Network{s.snapshot()}
}

func (s *Service) SetDown(networkID string, down bool) error {
	if networkID != s.seed.ID {
		return ErrNetworkNotFound
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.down = down
	return nil
}

func (s *Service) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.down = s.seed.Down
}

func (s *Service) snapshot() domain.Network {
	s.mu.RLock()
	defer s.mu.RUnlock()
	state := s.seed
	state.Down = s.down
	return state
}
