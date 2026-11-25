package network

import (
	"errors"
	"net/netip"
)

var (
	ErrSubnetAllocationPool        = errors.New("allocation pool requires IPv4 addresses in ascending order")
	ErrSubnetAllocationPoolBounds  = errors.New("allocation pool must be within the usable subnet address range")
	ErrSubnetAllocationPoolGateway = errors.New("allocation pool overlaps the subnet gateway")
)

type SubnetAllocationPool struct {
	Start netip.Addr
	End   netip.Addr
}

func subnetAllocationPool(
	prefix netip.Prefix,
	gateway netip.Addr,
	requested *SubnetAllocationPool,
) (SubnetAllocationPool, error) {
	last := lastUsableAddress(prefix)
	if requested == nil {
		return SubnetAllocationPool{Start: gateway.Next(), End: last}, nil
	}
	if !requested.Start.Is4() || !requested.End.Is4() || requested.Start.Compare(requested.End) > 0 {
		return SubnetAllocationPool{}, ErrSubnetAllocationPool
	}
	if requested.Start.Compare(prefix.Addr().Next()) < 0 || requested.End.Compare(last) > 0 {
		return SubnetAllocationPool{}, ErrSubnetAllocationPoolBounds
	}
	if gateway.Compare(requested.Start) >= 0 && gateway.Compare(requested.End) <= 0 {
		return SubnetAllocationPool{}, ErrSubnetAllocationPoolGateway
	}
	return *requested, nil
}
