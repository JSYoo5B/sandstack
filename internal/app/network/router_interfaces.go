package network

import (
	"errors"

	domain "github.com/JSYoo5B/sandstack/internal/domain/network"
)

var (
	ErrRouterSubnetAttached    = errors.New("subnet is already attached to the router")
	ErrRouterSubnetOverlap     = errors.New("subnet CIDR overlaps a router interface")
	ErrRouterGatewayInUse      = errors.New("subnet gateway IP is already in use")
	ErrRouterSubnetNoGateway   = errors.New("the subnet has no gateway IP address")
	ErrRouterInterfaceNotFound = errors.New("router interface for subnet not found")
)

func (s *Service) AddRouterInterface(projectID, routerID, subnetID string) (domain.Port, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	subnet, err := s.routerSubnet(projectID, routerID, subnetID)
	if err != nil {
		return domain.Port{}, err
	}
	if !subnet.Gateway.IsValid() {
		return domain.Port{}, ErrRouterSubnetNoGateway
	}
	if !isUsableIPv4(subnet, subnet.Gateway) {
		return domain.Port{}, ErrPortInvalidIPForSubnet
	}

	gatewayInUse := false
	usedMACs := make(map[string]bool)
	for _, port := range s.ports {
		if port.NetworkID == subnet.NetworkID {
			usedMACs[port.MACAddress] = true
		}
		if port.SubnetID == subnetID && port.FixedIP == subnet.Gateway {
			gatewayInUse = true
		}
		if err := s.checkRouterSubnetConflict(routerID, subnet, port); err != nil {
			return domain.Port{}, err
		}
	}
	if gatewayInUse {
		return domain.Port{}, ErrRouterGatewayInUse
	}

	return s.createPort(domain.Port{
		NetworkID:   subnet.NetworkID,
		ProjectID:   projectID,
		DeviceID:    routerID,
		DeviceOwner: domain.DeviceOwnerRouterInterface,
		SubnetID:    subnetID,
		FixedIP:     subnet.Gateway,
	}, usedMACs)
}

func (s *Service) RemoveRouterInterface(projectID, routerID, subnetID string) (domain.Port, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.routerSubnet(projectID, routerID, subnetID); err != nil {
		return domain.Port{}, err
	}
	for id, port := range s.ports {
		if port.DeviceOwner == domain.DeviceOwnerRouterInterface &&
			port.DeviceID == routerID && port.SubnetID == subnetID {
			if s.routerRoutesUseSubnet(routerID, subnetID) {
				return domain.Port{}, ErrRouterInterfaceInUseByRoute
			}
			delete(s.ports, id)
			return clonePort(port), nil
		}
	}
	return domain.Port{}, ErrRouterInterfaceNotFound
}

func (s *Service) checkRouterSubnetConflict(routerID string, subnet domain.Subnet, port domain.Port) error {
	if port.DeviceOwner != domain.DeviceOwnerRouterInterface || port.DeviceID != routerID {
		return nil
	}
	if port.SubnetID == subnet.ID {
		return ErrRouterSubnetAttached
	}
	if subnet.CIDR.Overlaps(s.subnets[port.SubnetID].CIDR) {
		return ErrRouterSubnetOverlap
	}
	return nil
}

// routerSubnet checks ownership of both resources while the write lock is held.
func (s *Service) routerSubnet(projectID, routerID, subnetID string) (domain.Subnet, error) {
	if !s.ownsRouter(projectID, routerID) {
		return domain.Subnet{}, ErrRouterNotFound
	}
	subnet, ok := s.subnets[subnetID]
	if !ok || subnet.ProjectID != projectID {
		return domain.Subnet{}, ErrSubnetNotFound
	}
	return subnet, nil
}

func (s *Service) ownsRouter(projectID, routerID string) bool {
	router, ok := s.routers[routerID]
	return ok && router.ProjectID == projectID
}
