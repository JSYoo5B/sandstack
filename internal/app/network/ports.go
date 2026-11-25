package network

import (
	"errors"
	"net/netip"
	"slices"
	"strings"

	domain "github.com/JSYoo5B/sandstack/internal/domain/network"
)

var (
	ErrPortNotFound            = errors.New("port not found")
	ErrPortSubnetRequired      = errors.New("port requires a subnet selection on a network with multiple subnets")
	ErrPortInvalidSubnet       = errors.New("port subnet does not belong to the network")
	ErrPortIPExhausted         = errors.New("port subnet allocation pool is exhausted")
	ErrServicePortInUse        = errors.New("port is managed by a network service")
	ErrPortInvalidIPForNetwork = errors.New("port address is not usable on a subnet of the network")
	ErrPortInvalidIPForSubnet  = errors.New("port address is not usable on the specified subnet")
	ErrPortIPAlreadyAllocated  = errors.New("port address is already allocated on the subnet")
)

func (s *Service) CreatePort(
	projectID, networkID, name string,
	adminDown bool,
	subnetID *string,
) (domain.Port, error) {
	return s.createRequestedPort(projectID, networkID, name, adminDown, subnetID, nil)
}

func (s *Service) CreatePortWithFixedIP(
	projectID, networkID, name string,
	adminDown bool,
	subnetID *string,
	address netip.Addr,
) (domain.Port, error) {
	return s.createRequestedPort(projectID, networkID, name, adminDown, subnetID, &address)
}

func (s *Service) createRequestedPort(
	projectID, networkID, name string,
	adminDown bool,
	subnetID *string,
	address *netip.Addr,
) (domain.Port, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	network, ok := s.networks[networkID]
	if !ok || network.ProjectID != projectID {
		return domain.Port{}, ErrNetworkNotFound
	}
	subnet, err := s.selectPortSubnet(network, subnetID, address)
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
	fixedIP, err := selectPortAddress(subnet, address, usedIPs)
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

type PortListFilter struct {
	IDs            []string
	Names          []string
	AdminStates    []bool
	NetworkIDs     []string
	DeviceIDs      []string
	DeviceOwners   []string
	FixedAddresses []string
	FixedSubnetIDs []string
}

func (f PortListFilter) matches(port domain.Port) bool {
	return (len(f.IDs) == 0 || slices.Contains(f.IDs, port.ID)) &&
		(len(f.Names) == 0 || slices.Contains(f.Names, port.Name)) &&
		(len(f.AdminStates) == 0 || slices.Contains(f.AdminStates, !port.AdminDown)) &&
		(len(f.NetworkIDs) == 0 || slices.Contains(f.NetworkIDs, port.NetworkID)) &&
		(len(f.DeviceIDs) == 0 || slices.Contains(f.DeviceIDs, port.DeviceID)) &&
		(len(f.DeviceOwners) == 0 || slices.Contains(f.DeviceOwners, port.DeviceOwner)) &&
		(len(f.FixedAddresses) == 0 ||
			(port.FixedIP.IsValid() && slices.Contains(f.FixedAddresses, port.FixedIP.String()))) &&
		(len(f.FixedSubnetIDs) == 0 ||
			(port.SubnetID != "" && slices.Contains(f.FixedSubnetIDs, port.SubnetID)))
}

// ListPorts returns the project's matching ports ordered by ID.
func (s *Service) ListPorts(projectID string, filter PortListFilter) []domain.Port {
	ports, _ := s.listPortsAfter(projectID, filter, "")
	return ports
}

// ListPortsPage returns a forward page and whether more matching ports follow.
func (s *Service) ListPortsPage(
	projectID string,
	filter PortListFilter,
	marker string,
	limit int,
) ([]domain.Port, bool, error) {
	if limit <= 0 {
		return s.ListPorts(projectID, filter), false, nil
	}
	ports, err := s.listPortsAfter(projectID, filter, marker)
	if err != nil {
		return nil, false, err
	}
	if len(ports) > limit {
		return ports[:limit], true, nil
	}
	return ports, false, nil
}

func (s *Service) listPortsAfter(projectID string, filter PortListFilter, marker string) ([]domain.Port, error) {
	ports := []domain.Port{}
	s.mu.RLock()
	if marker != "" {
		port, exists := s.ports[marker]
		if !exists || port.ProjectID != projectID {
			s.mu.RUnlock()
			return nil, ErrPortNotFound
		}
	}
	for _, port := range s.ports {
		if port.ProjectID == projectID && port.ID > marker && filter.matches(port) {
			ports = append(ports, port)
		}
	}
	s.mu.RUnlock()
	slices.SortFunc(ports, func(a, b domain.Port) int { return strings.Compare(a.ID, b.ID) })
	return ports, nil
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
