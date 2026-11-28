package network

import (
	"errors"
	"net/netip"

	domain "github.com/JSYoo5B/sandstack/internal/domain/network"
)

var (
	ErrNetworkNotExternal = errors.New("network is not an external network")
)

type NetworkNotFoundError struct{ ID string }

func (e *NetworkNotFoundError) Error() string {
	return ErrNetworkNotFound.Error() + ": " + e.ID
}

func (e *NetworkNotFoundError) Unwrap() error { return ErrNetworkNotFound }

type RouterGatewayUpdate struct {
	NetworkID string
}

func (s *Service) applyRouterGateway(router *domain.Router, change RouterGatewayUpdate) (func(), error) {
	current := router.Gateway
	if change.NetworkID != "" {
		network, ok := s.networks[change.NetworkID]
		if !ok || !visibleNetwork(network, router.ProjectID) {
			return nil, &NetworkNotFoundError{ID: change.NetworkID}
		}
		if !network.External {
			return nil, ErrNetworkNotExternal
		}
	}
	if current != nil && current.NetworkID == change.NetworkID {
		return func() {}, nil
	}

	var removed *domain.Port
	if current != nil {
		if s.routerGatewayServesFloatingIPs(router.ID) {
			return nil, ErrRouterGatewayInUseByFloatingIP
		}
		port := s.ports[current.PortID]
		removed = &port
		delete(s.ports, current.PortID)
		router.Gateway = nil
	}
	restore := func() {
		if removed != nil {
			s.ports[removed.ID] = *removed
		}
	}
	if change.NetworkID == "" {
		return restore, nil
	}

	gateway, err := s.createRouterGatewayPort(router.ID, s.networks[change.NetworkID])
	if err != nil {
		restore()
		router.Gateway = current
		return nil, err
	}
	router.Gateway = &gateway
	return func() {
		delete(s.ports, gateway.PortID)
		restore()
	}, nil
}

func (s *Service) createRouterGatewayPort(routerID string, network domain.Network) (domain.RouterGateway, error) {
	for _, port := range s.ports {
		for _, subnetID := range network.SubnetIDs {
			if err := s.checkRouterSubnetConflict(routerID, s.subnets[subnetID], port); err != nil {
				return domain.RouterGateway{}, err
			}
		}
	}

	var subnet domain.Subnet
	if len(network.SubnetIDs) > 0 {
		subnet = s.subnets[network.SubnetIDs[0]]
	}
	usedMACs := make(map[string]bool)
	usedIPs := make(map[netip.Addr]bool)
	for _, port := range s.ports {
		if port.NetworkID == network.ID {
			usedMACs[port.MACAddress] = true
		}
		if subnet.ID != "" && port.SubnetID == subnet.ID {
			usedIPs[port.FixedIP] = true
		}
	}
	address, err := selectPortAddress(subnet, nil, usedIPs)
	if err != nil {
		return domain.RouterGateway{}, err
	}
	port, err := s.createPort(domain.Port{
		NetworkID:            network.ID,
		DeviceID:             routerID,
		DeviceOwner:          domain.DeviceOwnerRouterGateway,
		SubnetID:             subnet.ID,
		FixedIP:              address,
		PortSecurityDisabled: true,
	}, usedMACs)
	if err != nil {
		return domain.RouterGateway{}, err
	}
	return domain.RouterGateway{
		NetworkID:  network.ID,
		PortID:     port.ID,
		SubnetID:   port.SubnetID,
		Address:    port.FixedIP,
		EnableSNAT: true,
	}, nil
}

func (s *Service) routerPortSubnets(routerID string, port domain.Port) []domain.Subnet {
	if port.DeviceID != routerID {
		return nil
	}
	switch port.DeviceOwner {
	case domain.DeviceOwnerRouterInterface:
		return []domain.Subnet{s.subnets[port.SubnetID]}
	case domain.DeviceOwnerRouterGateway:
		subnets := make([]domain.Subnet, 0, len(s.networks[port.NetworkID].SubnetIDs))
		for _, id := range s.networks[port.NetworkID].SubnetIDs {
			subnets = append(subnets, s.subnets[id])
		}
		return subnets
	}
	return nil
}

func cloneGateway(gateway *domain.RouterGateway) *domain.RouterGateway {
	if gateway == nil {
		return nil
	}
	copied := *gateway
	return &copied
}
