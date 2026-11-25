package network

import (
	"errors"
	"net/netip"

	domain "github.com/JSYoo5B/sandstack/internal/domain/network"
)

var ErrSubnetGateway = errors.New("the gateway must be an IPv4 address other than the subnet network or broadcast address")
var ErrSubnetGatewayAttached = errors.New("the existing gateway IP is assigned to a router interface")

func subnetGateway(prefix netip.Prefix, requested *netip.Addr) (netip.Addr, error) {
	if requested == nil {
		return prefix.Addr().Next(), nil
	}
	gateway := *requested
	if !gateway.IsValid() {
		return netip.Addr{}, nil
	}
	if !gateway.Is4() || gateway == prefix.Addr() || gateway == lastUsableAddress(prefix).Next() {
		return netip.Addr{}, ErrSubnetGateway
	}
	return gateway, nil
}

// changeSubnetGateway validates the new gateway with the service write lock held.
func (s *Service) changeSubnetGateway(subnet domain.Subnet, requested *netip.Addr) (netip.Addr, error) {
	gateway, err := subnetGateway(subnet.CIDR, requested)
	if err != nil || gateway == subnet.Gateway {
		return gateway, err
	}
	if subnet.Gateway.IsValid() {
		for _, port := range s.ports {
			if port.SubnetID == subnet.ID && port.FixedIP == subnet.Gateway &&
				port.DeviceOwner == domain.DeviceOwnerRouterInterface {
				return netip.Addr{}, ErrSubnetGatewayAttached
			}
		}
	}
	if err := validateGatewayOutsidePools(gateway, subnet.AllocationPools); err != nil {
		return netip.Addr{}, err
	}
	return gateway, nil
}
