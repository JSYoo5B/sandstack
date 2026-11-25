package network

import (
	"errors"
	"net/netip"
	"slices"

	domain "github.com/JSYoo5B/sandstack/internal/domain/network"
)

var (
	ErrSubnetAllocationPool        = errors.New("allocation pool requires IPv4 addresses in ascending order")
	ErrSubnetAllocationPoolBounds  = errors.New("allocation pool must be within the usable subnet address range")
	ErrSubnetAllocationPoolGateway = errors.New("allocation pool overlaps the subnet gateway")
	ErrSubnetAllocationPoolOverlap = errors.New("subnet allocation pools overlap")
)

func subnetAllocationPools(
	prefix netip.Prefix,
	gateway netip.Addr,
	requested []domain.AllocationPool,
) ([]domain.AllocationPool, error) {
	first := prefix.Addr().Next()
	last := lastUsableAddress(prefix)
	if len(requested) == 0 {
		return []domain.AllocationPool{{Start: gateway.Next(), End: last}}, nil
	}
	for _, pool := range requested {
		if !pool.Start.Is4() || !pool.End.Is4() || pool.Start.Compare(pool.End) > 0 {
			return nil, ErrSubnetAllocationPool
		}
		if pool.Start.Compare(first) < 0 || pool.End.Compare(last) > 0 {
			return nil, ErrSubnetAllocationPoolBounds
		}
	}
	pools := slices.Clone(requested)
	slices.SortFunc(pools, func(a, b domain.AllocationPool) int { return a.Start.Compare(b.Start) })
	for i := 1; i < len(pools); i++ {
		if pools[i].Start.Compare(pools[i-1].End) <= 0 {
			return nil, ErrSubnetAllocationPoolOverlap
		}
	}
	for _, pool := range pools {
		if gateway.Compare(pool.Start) >= 0 && gateway.Compare(pool.End) <= 0 {
			return nil, ErrSubnetAllocationPoolGateway
		}
	}
	return pools, nil
}
