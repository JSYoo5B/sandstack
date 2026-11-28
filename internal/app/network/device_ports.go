package network

import (
	"cmp"
	"net/netip"
	"slices"
	"strings"

	domain "github.com/JSYoo5B/sandstack/internal/domain/network"
)

// AvailableNetworks returns the project's own networks that are not external, ordered by ID.
func (s *Service) AvailableNetworks(projectID string) []domain.Network {
	s.mu.RLock()
	defer s.mu.RUnlock()

	networks := []domain.Network{}
	for _, network := range s.networks {
		if network.ProjectID == projectID && !network.External {
			networks = append(networks, snapshot(network))
		}
	}
	slices.SortFunc(networks, func(a, b domain.Network) int { return strings.Compare(a.ID, b.ID) })
	return networks
}

// ProjectSecurityGroups returns the project's security groups ordered by ID.
func (s *Service) ProjectSecurityGroups(projectID string) []domain.SecurityGroup {
	groups := s.ListSecurityGroups(projectID, SecurityGroupFilter{})
	slices.SortFunc(groups, func(a, b domain.SecurityGroup) int { return cmp.Compare(a.ID, b.ID) })
	return groups
}

// CreateDevicePort creates a port bound to a device. A nil address selects
// the next free address and nil groups select the project's default group.
func (s *Service) CreateDevicePort(
	projectID, networkID string,
	address *netip.Addr,
	securityGroups *[]string,
	deviceID, deviceOwner string,
) (domain.Port, error) {
	port, err := s.CreatePortWithOptions(projectID, networkID, "", false, nil, address, securityGroups)
	if err != nil {
		return domain.Port{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	stored, ok := s.ports[port.ID]
	if !ok {
		return domain.Port{}, ErrPortNotFound
	}
	stored.DeviceID = deviceID
	stored.DeviceOwner = deviceOwner
	s.ports[port.ID] = stored
	return clonePort(stored), nil
}

// BindPort binds an unbound port to a device.
func (s *Service) BindPort(projectID, portID, deviceID, deviceOwner string) (domain.Port, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	port, ok := s.ports[portID]
	if !ok || port.ProjectID != projectID {
		return domain.Port{}, ErrPortNotFound
	}
	if port.DeviceID != "" {
		return domain.Port{}, ErrPortInUse
	}
	port.DeviceID = deviceID
	port.DeviceOwner = deviceOwner
	s.ports[portID] = port
	return clonePort(port), nil
}

// UnbindPort clears the device binding when the port is still bound to the device.
func (s *Service) UnbindPort(projectID, portID, deviceID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	port, ok := s.ports[portID]
	if !ok || port.ProjectID != projectID {
		return ErrPortNotFound
	}
	if port.DeviceID == deviceID {
		port.DeviceID = ""
		port.DeviceOwner = ""
		s.ports[portID] = port
	}
	return nil
}

// DescribePort returns the port with its network name, floating addresses,
// and security group names.
func (s *Service) DescribePort(projectID, portID string) (domain.DeviceInterface, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	port, ok := s.ports[portID]
	if !ok || port.ProjectID != projectID {
		return domain.DeviceInterface{}, ErrPortNotFound
	}
	described := domain.DeviceInterface{
		Port:        clonePort(port),
		NetworkName: s.networks[port.NetworkID].Name,
	}
	floatingIPs := slices.SortedFunc(func(yield func(domain.FloatingIP) bool) {
		for _, floatingIP := range s.floatingIPs {
			if floatingIP.PortID == portID && !yield(floatingIP) {
				return
			}
		}
	}, func(a, b domain.FloatingIP) int { return a.FloatingAddress.Compare(b.FloatingAddress) })
	for _, floatingIP := range floatingIPs {
		described.FloatingAddresses = append(described.FloatingAddresses, floatingIP.FloatingAddress)
	}
	for _, groupID := range port.SecurityGroups {
		name := s.securityGroups[groupID].Name
		if name == "" {
			name = groupID
		}
		described.SecurityGroupNames = append(described.SecurityGroupNames, name)
	}
	return described, nil
}

// AddressDevice reports whether a project port on the network holds the
// address and returns the device it is bound to.
func (s *Service) AddressDevice(projectID, networkID string, address netip.Addr) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, port := range s.ports {
		if port.ProjectID == projectID && port.NetworkID == networkID && port.FixedIP == address {
			return port.DeviceID, true
		}
	}
	return "", false
}

func (s *Service) SetPortSecurityGroups(projectID, portID string, groupIDs []string) (domain.Port, error) {
	groups := slices.Clone(groupIDs)
	return s.UpdatePort(projectID, portID, PortUpdate{SecurityGroups: &groups})
}

func (s *Service) DevicePorts(projectID, deviceID string) []domain.Port {
	return s.ListPorts(projectID, PortListFilter{DeviceIDs: []string{deviceID}})
}
