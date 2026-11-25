package network

import (
	"errors"
	"net/netip"
	"slices"
	"strings"

	domain "github.com/JSYoo5B/sandstack/internal/domain/network"
)

var (
	ErrPortNotFound       = errors.New("port not found")
	ErrPortSubnetRequired = errors.New("port requires a subnet selection on a network with multiple subnets")
	ErrPortInvalidSubnet  = errors.New("port subnet does not belong to the network")
	ErrPortIPExhausted    = errors.New("port subnet allocation pool is exhausted")
	ErrServicePortInUse   = errors.New("port is managed by a network service")
)

func (s *Service) CreatePort(
	projectID, networkID, name string,
	adminDown bool,
	subnetID *string,
) (domain.Port, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	network, ok := s.networks[networkID]
	if !ok || network.ProjectID != projectID {
		return domain.Port{}, ErrNetworkNotFound
	}
	subnet, err := s.portSubnet(network, subnetID)
	if err != nil {
		return domain.Port{}, err
	}

	usedMACs := make(map[string]bool)
	usedIPs := make(map[netip.Addr]bool)
	for _, port := range s.ports {
		if port.NetworkID == networkID {
			usedMACs[port.MACAddress] = true
		}
		if subnet.ID != "" && port.SubnetID == subnet.ID {
			usedIPs[port.FixedIP] = true
		}
	}
	fixedIP, err := firstFreeAddress(subnet, usedIPs)
	if err != nil {
		return domain.Port{}, err
	}
	return s.createPort(domain.Port{
		NetworkID: networkID,
		ProjectID: network.ProjectID,
		Name:      name,
		SubnetID:  subnet.ID,
		FixedIP:   fixedIP,
		AdminDown: adminDown,
	}, usedMACs)
}

// createPort allocates identifiers with the service's write lock held.
func (s *Service) createPort(port domain.Port, usedMACs map[string]bool) (domain.Port, error) {
	id, err := s.allocateID(func(id string) bool { return !usedMACs[portMAC(id)] })
	if err != nil {
		return domain.Port{}, err
	}
	port.ID = id
	port.MACAddress = portMAC(id)
	s.ports[id] = port
	return port, nil
}

func (s *Service) GetPort(projectID, portID string) (domain.Port, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	port, ok := s.ports[portID]
	if !ok || port.ProjectID != projectID {
		return domain.Port{}, ErrPortNotFound
	}
	return port, nil
}

// ListPorts returns the project's matching ports ordered by ID.
func (s *Service) ListPorts(projectID string, networkIDs []string) []domain.Port {
	ports := []domain.Port{}
	s.mu.RLock()
	for _, port := range s.ports {
		if port.ProjectID == projectID &&
			(len(networkIDs) == 0 || slices.Contains(networkIDs, port.NetworkID)) {
			ports = append(ports, port)
		}
	}
	s.mu.RUnlock()
	slices.SortFunc(ports, func(a, b domain.Port) int { return strings.Compare(a.ID, b.ID) })
	return ports
}

func (s *Service) DeletePort(projectID, portID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	port, ok := s.ports[portID]
	if !ok || port.ProjectID != projectID {
		return ErrPortNotFound
	}
	if port.DeviceOwner == domain.DeviceOwnerRouterInterface {
		return ErrServicePortInUse
	}
	delete(s.ports, portID)
	return nil
}
