package network

import (
	"errors"
	"net/netip"
	"slices"

	domain "github.com/JSYoo5B/sandstack/internal/domain/network"
)

var (
	ErrPortNotFound            = errors.New("port not found")
	ErrPortSubnetRequired      = errors.New("port requires a subnet selection on a network with multiple subnets")
	ErrPortInvalidSubnet       = errors.New("port subnet does not belong to the network")
	ErrPortIPExhausted         = domain.ErrPortIPExhausted
	ErrServicePortInUse        = errors.New("port is managed by a network service")
	ErrPortInvalidIPForNetwork = errors.New("port address is not usable on a subnet of the network")
	ErrPortInvalidIPForSubnet  = errors.New("port address is not usable on the specified subnet")
	ErrPortIPAlreadyAllocated  = domain.ErrPortIPAlreadyAllocated
)

func (s *Service) CreatePort(
	projectID, networkID, name string,
	adminDown bool,
	subnetID *string,
) (domain.Port, error) {
	return s.CreatePortWithOptions(projectID, networkID, name, adminDown, subnetID, nil, nil)
}

func (s *Service) CreatePortWithFixedIP(
	projectID, networkID, name string,
	adminDown bool,
	subnetID *string,
	address netip.Addr,
) (domain.Port, error) {
	return s.CreatePortWithOptions(projectID, networkID, name, adminDown, subnetID, &address, nil)
}

func (s *Service) CreatePortWithOptions(
	projectID, networkID, name string,
	adminDown bool,
	subnetID *string,
	address *netip.Addr,
	securityGroups *[]string,
) (domain.Port, error) {
	return s.CreatePortWithSecurity(
		projectID, networkID, name, adminDown,
		subnetID, address, securityGroups, PortSecurity{},
	)
}

func (s *Service) CreatePortWithSecurity(
	projectID, networkID, name string,
	adminDown bool,
	subnetID *string,
	address *netip.Addr,
	securityGroups *[]string,
	security PortSecurity,
) (domain.Port, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	network, ok := s.networks[networkID]
	if !ok || network.ProjectID != projectID {
		return domain.Port{}, ErrNetworkNotFound
	}
	enabled := !network.PortSecurityDisabled
	if security.Enabled != nil {
		enabled = *security.Enabled
	}
	if err := checkCreatePortSecurity(enabled, security, securityGroups); err != nil {
		return domain.Port{}, err
	}
	if !enabled && securityGroups == nil {
		securityGroups = &[]string{}
	}
	groupIDs, err := s.portSecurityGroups(projectID, securityGroups)
	if err != nil {
		return domain.Port{}, err
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
	var pairs []domain.AddressPair
	if security.AllowedAddressPairs != nil {
		pairs = *security.AllowedAddressPairs
	}
	return s.createPort(domain.Port{
		NetworkID:            networkID,
		ProjectID:            network.ProjectID,
		Name:                 name,
		SubnetID:             subnet.ID,
		FixedIP:              fixedIP,
		AdminDown:            adminDown,
		SecurityGroups:       groupIDs,
		PortSecurityDisabled: !enabled,
		AllowedAddressPairs:  pairs,
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
	port.AllowedAddressPairs, err = resolveAddressPairs(port.AllowedAddressPairs, port.MACAddress)
	if err != nil {
		return domain.Port{}, err
	}
	s.ports[id] = port
	return clonePort(port), nil
}

func (s *Service) GetPort(projectID, portID string) (domain.Port, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	port, ok := s.ports[portID]
	if !ok || port.ProjectID != projectID {
		return domain.Port{}, ErrPortNotFound
	}
	return clonePort(port), nil
}

func clonePort(port domain.Port) domain.Port {
	port.SecurityGroups = slices.Clone(port.SecurityGroups)
	port.AllowedAddressPairs = slices.Clone(port.AllowedAddressPairs)
	return port
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
	s.disassociatePortFloatingIPs(portID)
	delete(s.ports, portID)
	return nil
}
