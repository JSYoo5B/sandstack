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
	ErrNetworkForbidden    = errors.New("network is owned by another project")
)

const networkIDAttempts = 8

var networkIDPattern = regexp.MustCompile(`^[0-9a-f]{8}(-[0-9a-f]{4}){3}-[0-9a-f]{12}$`)

type Service struct {
	mu                      sync.RWMutex
	seed                    domain.Network
	external                domain.Network
	externalSubnet          domain.Subnet
	networks                map[string]domain.Network
	subnets                 map[string]domain.Subnet
	ports                   map[string]domain.Port
	routers                 map[string]domain.Router
	securityGroups          map[string]domain.SecurityGroup
	securityGroupRuleGroups map[string]string
	floatingIPs             map[string]domain.FloatingIP
	newID                   func() string
}

type ListFilter struct {
	Names       []string
	Statuses    []string
	AdminStates []bool
	External    []bool
}

// New starts a service with one private network and its default security group.
// The generator supplies a lowercase UUID on success or an empty string on failure.
func New(seed domain.Network, newID func() string) *Service {
	return NewWithExternalNetwork(seed, domain.Network{}, domain.Subnet{}, newID)
}

func NewWithExternalNetwork(seed, external domain.Network, subnet domain.Subnet, newID func() string) *Service {
	seed.SubnetIDs = nil
	external.SubnetIDs = nil
	external.External = external.ID != ""
	if external.External && subnet.ID != "" {
		subnet.NetworkID = external.ID
		subnet.ProjectID = external.ProjectID
		external.SubnetIDs = []string{subnet.ID}
	} else {
		subnet = domain.Subnet{}
	}
	s := &Service{
		seed:           seed,
		external:       external,
		externalSubnet: subnet,
		newID:          newID,
	}
	s.restoreFixtures()
	return s
}

func (s *Service) restoreFixtures() {
	s.networks = map[string]domain.Network{s.seed.ID: s.seed}
	s.subnets = make(map[string]domain.Subnet)
	if s.external.External {
		s.networks[s.external.ID] = snapshot(s.external)
	}
	if s.externalSubnet.ID != "" {
		s.subnets[s.externalSubnet.ID] = copySubnet(s.externalSubnet)
	}
	s.ports = make(map[string]domain.Port)
	s.routers = make(map[string]domain.Router)
	s.floatingIPs = make(map[string]domain.FloatingIP)
	s.resetSecurityGroups()
}

func visibleNetwork(network domain.Network, projectID string) bool {
	return network.ProjectID == projectID || network.External
}

func (s *Service) Get(projectID, networkID string) (domain.Network, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	network, ok := s.networks[networkID]
	if !ok || !visibleNetwork(network, projectID) {
		return domain.Network{}, ErrNetworkNotFound
	}
	return snapshot(network), nil
}

// List returns the project's matching networks ordered by ID.
func (s *Service) List(projectID string, filter ListFilter) []domain.Network {
	page, _ := s.ListNetworksPage(projectID, filter, PageOptions{})
	return page.Items
}

// ListNetworksPage windows the project's matching networks in ascending ID order.
func (s *Service) ListNetworksPage(projectID string, filter ListFilter, options PageOptions) (Page[domain.Network], error) {
	networks := []domain.Network{}
	var marker *domain.Network
	s.mu.RLock()
	if options.Limit > 0 && options.Marker != "" {
		network, exists := s.networks[options.Marker]
		if !exists || !visibleNetwork(network, projectID) {
			s.mu.RUnlock()
			return Page[domain.Network]{}, ErrNetworkNotFound
		}
		marker = &network
	}
	for _, network := range s.networks {
		if !visibleNetwork(network, projectID) ||
			(len(filter.External) > 0 && !slices.Contains(filter.External, network.External)) ||
			(len(filter.Names) > 0 && !slices.Contains(filter.Names, network.Name)) ||
			(len(filter.Statuses) > 0 && !slices.Contains(filter.Statuses, network.Status())) ||
			(len(filter.AdminStates) > 0 && !slices.Contains(filter.AdminStates, !network.AdminDown)) {
			continue
		}
		networks = append(networks, snapshot(network))
	}
	s.mu.RUnlock()

	return paginate(networks, options, marker,
		func(a, b domain.Network) int { return strings.Compare(a.ID, b.ID) },
		func(network domain.Network) string { return network.ID }), nil
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

	network, err := s.ownedNetwork(projectID, networkID)
	if err != nil {
		return domain.Network{}, err
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

func (s *Service) ownedNetwork(projectID, networkID string) (domain.Network, error) {
	network, ok := s.networks[networkID]
	if !ok || !visibleNetwork(network, projectID) {
		return domain.Network{}, ErrNetworkNotFound
	}
	if network.ProjectID != projectID {
		return domain.Network{}, ErrNetworkForbidden
	}
	return network, nil
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
		_, groupExists := s.securityGroups[id]
		_, ruleExists := s.securityGroupRuleGroups[id]
		_, floatingIPExists := s.floatingIPs[id]
		if networkExists || subnetExists || portExists || routerExists || id == s.seed.ID ||
			id == s.external.ID || id == s.externalSubnet.ID ||
			groupExists || ruleExists || floatingIPExists || isDefaultSecurityGroupID(id) ||
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

	network, err := s.ownedNetwork(projectID, networkID)
	if err != nil {
		return err
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

	s.restoreFixtures()
}

func snapshot(network domain.Network) domain.Network {
	network.SubnetIDs = append([]string(nil), network.SubnetIDs...)
	return network
}
