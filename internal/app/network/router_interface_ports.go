package network

import (
	"errors"

	domain "github.com/JSYoo5B/sandstack/internal/domain/network"
)

var (
	ErrPortInUse                     = errors.New("port is already bound to a device")
	ErrRouterPortNoFixedIP           = errors.New("router interface port requires a fixed IPv4 address")
	ErrRouterInterfacePortNotFound   = errors.New("router interface for port not found")
	ErrRouterInterfaceSubnetMismatch = errors.New("port is not on the specified subnet")
)

func (s *Service) AddRouterInterfaceByPort(projectID, routerID, portID string) (domain.Port, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.ownsRouter(projectID, routerID) {
		return domain.Port{}, ErrRouterNotFound
	}
	port, ok := s.ports[portID]
	if !ok || port.ProjectID != projectID {
		return domain.Port{}, ErrPortNotFound
	}
	if port.DeviceID != "" || port.DeviceOwner != "" {
		return domain.Port{}, ErrPortInUse
	}
	subnet, ok := s.subnets[port.SubnetID]
	if !ok || subnet.ProjectID != projectID || subnet.NetworkID != port.NetworkID ||
		!port.FixedIP.Is4() || !subnet.CIDR.Contains(port.FixedIP) {
		return domain.Port{}, ErrRouterPortNoFixedIP
	}
	for _, existing := range s.ports {
		if err := s.checkRouterSubnetConflict(routerID, subnet, existing); err != nil {
			return domain.Port{}, err
		}
	}
	port.DeviceID = routerID
	port.DeviceOwner = domain.DeviceOwnerRouterInterface
	s.ports[portID] = port
	return clonePort(port), nil
}

func (s *Service) RemoveRouterInterfaceByPort(projectID, routerID, portID, subnetID string) (domain.Port, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.ownsRouter(projectID, routerID) {
		return domain.Port{}, ErrRouterNotFound
	}
	port, ok := s.ports[portID]
	if !ok || port.ProjectID != projectID || port.DeviceID != routerID ||
		port.DeviceOwner != domain.DeviceOwnerRouterInterface {
		return domain.Port{}, ErrRouterInterfacePortNotFound
	}
	if subnetID != "" && subnetID != port.SubnetID {
		return domain.Port{}, ErrRouterInterfaceSubnetMismatch
	}
	if s.routerRoutesUseSubnet(routerID, port.SubnetID) {
		return domain.Port{}, ErrRouterInterfaceInUseByRoute
	}
	if s.routerInterfaceServesFloatingIPs(routerID, port.SubnetID) {
		return domain.Port{}, ErrRouterInterfaceInUseByFloatingIP
	}
	delete(s.ports, portID)
	return clonePort(port), nil
}
