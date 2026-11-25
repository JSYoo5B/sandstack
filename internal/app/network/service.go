package network

import (
	"errors"
	"regexp"
	"slices"
	"strings"
	"sync"

	domain "github.com/JSYoo5B/sandstack/internal/domain/network"
)

var (
	ErrNetworkNotFound     = errors.New("network not found")
	ErrNetworkIDGeneration = errors.New("network ID generation failed")
)

const networkIDAttempts = 8

var networkIDPattern = regexp.MustCompile(`^[0-9a-f]{8}(-[0-9a-f]{4}){3}-[0-9a-f]{12}$`)

type Service struct {
	mu       sync.RWMutex
	seed     domain.Network
	networks map[string]domain.Network
	newID    func() string
}

type ListFilter struct {
	Names    []string
	Statuses []string
}

// New starts a service with a private fixture. The generator supplies a
// lowercase UUID for each created network, or an empty string on failure.
func New(seed domain.Network, newID func() string) *Service {
	return &Service{
		seed:     seed,
		networks: map[string]domain.Network{seed.ID: seed},
		newID:    newID,
	}
}

func (s *Service) Get(projectID, networkID string) (domain.Network, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	network, ok := s.networks[networkID]
	if !ok || network.ProjectID != projectID {
		return domain.Network{}, ErrNetworkNotFound
	}
	return network, nil
}

// List returns the project's matching networks ordered by ID.
func (s *Service) List(projectID string, filter ListFilter) []domain.Network {
	networks := []domain.Network{}
	s.mu.RLock()
	for _, network := range s.networks {
		if network.ProjectID != projectID ||
			(len(filter.Names) > 0 && !slices.Contains(filter.Names, network.Name)) ||
			(len(filter.Statuses) > 0 && !slices.Contains(filter.Statuses, network.Status())) {
			continue
		}
		networks = append(networks, network)
	}
	s.mu.RUnlock()

	slices.SortFunc(networks, func(a, b domain.Network) int { return strings.Compare(a.ID, b.ID) })
	return networks
}

func (s *Service) Create(projectID, name string, adminDown bool) (domain.Network, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.newID == nil {
		return domain.Network{}, ErrNetworkIDGeneration
	}
	for range networkIDAttempts {
		id := s.newID()
		if !networkIDPattern.MatchString(id) {
			return domain.Network{}, ErrNetworkIDGeneration
		}
		if _, exists := s.networks[id]; exists || id == s.seed.ID {
			continue
		}
		network := domain.Network{ID: id, Name: name, ProjectID: projectID, AdminDown: adminDown}
		s.networks[id] = network
		return network, nil
	}
	return domain.Network{}, ErrNetworkIDGeneration
}

func (s *Service) Delete(projectID, networkID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	network, ok := s.networks[networkID]
	if !ok || network.ProjectID != projectID {
		return ErrNetworkNotFound
	}
	delete(s.networks, networkID)
	return nil
}

func (s *Service) SetDown(networkID string, down bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	network, ok := s.networks[networkID]
	if !ok {
		return ErrNetworkNotFound
	}
	network.Down = down
	s.networks[networkID] = network
	return nil
}

func (s *Service) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.networks = map[string]domain.Network{s.seed.ID: s.seed}
}
