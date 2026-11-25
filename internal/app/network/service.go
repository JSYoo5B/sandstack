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
	ErrNetworkInUse        = errors.New("network has attached ports")
)

const networkIDAttempts = 8

var networkIDPattern = regexp.MustCompile(`^[0-9a-f]{8}(-[0-9a-f]{4}){3}-[0-9a-f]{12}$`)

type Service struct {
	mu       sync.RWMutex
	seed     domain.Network
	networks map[string]domain.Network
	subnets  map[string]domain.Subnet
	ports    map[string]domain.Port
	routers  map[string]domain.Router
	newID    func() string
}

type ListFilter struct {
	Names    []string
	Statuses []string
}

// New starts a service with one private network. The generator
// supplies a lowercase UUID for each created resource, or an empty string on failure.
func New(seed domain.Network, newID func() string) *Service {
	seed.SubnetIDs = nil
	return &Service{
		seed:     seed,
		networks: map[string]domain.Network{seed.ID: seed},
		subnets:  make(map[string]domain.Subnet),
		ports:    make(map[string]domain.Port),
		routers:  make(map[string]domain.Router),
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
	return snapshot(network), nil
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
		networks = append(networks, snapshot(network))
	}
	s.mu.RUnlock()

	slices.SortFunc(networks, func(a, b domain.Network) int { return strings.Compare(a.ID, b.ID) })
	return networks
}

func (s *Service) Create(projectID, name string, adminDown bool) (domain.Network, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	id, err := s.allocateID(nil)
	if err != nil {
		return domain.Network{}, err
	}
	network := domain.Network{ID: id, Name: name, ProjectID: projectID, AdminDown: adminDown}
	s.networks[id] = network
	return network, nil
}

type NetworkUpdate struct {
	Name         *string
	AdminStateUp *bool
}

func (s *Service) Update(projectID, networkID string, changes NetworkUpdate) (domain.Network, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	network, ok := s.networks[networkID]
	if !ok || network.ProjectID != projectID {
		return domain.Network{}, ErrNetworkNotFound
	}
	if changes.Name != nil {
		network.Name = *changes.Name
	}
	if changes.AdminStateUp != nil {
		network.AdminDown = !*changes.AdminStateUp
	}
	s.networks[networkID] = network
	return snapshot(network), nil
}

// allocateID runs with the write lock held. If supplied, available can reject
// a candidate that conflicts with a resource attribute derived from its ID.
func (s *Service) allocateID(available func(string) bool) (string, error) {
	if s.newID == nil {
		return "", ErrNetworkIDGeneration
	}
	for range networkIDAttempts {
		id := s.newID()
		if !networkIDPattern.MatchString(id) {
			return "", ErrNetworkIDGeneration
		}
		_, networkExists := s.networks[id]
		_, subnetExists := s.subnets[id]
		_, portExists := s.ports[id]
		_, routerExists := s.routers[id]
		if networkExists || subnetExists || portExists || routerExists || id == s.seed.ID ||
			(available != nil && !available(id)) {
			continue
		}
		return id, nil
	}
	return "", ErrNetworkIDGeneration
}

func (s *Service) Delete(projectID, networkID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	network, ok := s.networks[networkID]
	if !ok || network.ProjectID != projectID {
		return ErrNetworkNotFound
	}
	for _, port := range s.ports {
		if port.NetworkID == networkID {
			return ErrNetworkInUse
		}
	}
	for _, subnetID := range network.SubnetIDs {
		delete(s.subnets, subnetID)
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
	s.subnets = make(map[string]domain.Subnet)
	s.ports = make(map[string]domain.Port)
	s.routers = make(map[string]domain.Router)
}

func snapshot(network domain.Network) domain.Network {
	network.SubnetIDs = append([]string(nil), network.SubnetIDs...)
	return network
}
