package network

import (
	"errors"
	"net/netip"
)

var ErrSubnetGateway = errors.New("the gateway must be an IPv4 address other than the subnet network or broadcast address")

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
