package network

import (
	"crypto/sha256"
	"net"
	"net/netip"

	domain "github.com/JSYoo5B/sandstack/internal/domain/network"
)

func (s *Service) portSubnet(network domain.Network, requested *string) (domain.Subnet, error) {
	if requested == nil {
		switch len(network.SubnetIDs) {
		case 0:
			return domain.Subnet{}, nil
		case 1:
			return s.subnets[network.SubnetIDs[0]], nil
		default:
			return domain.Subnet{}, ErrPortSubnetRequired
		}
	}
	if *requested == "" {
		return domain.Subnet{}, nil
	}
	subnet, ok := s.subnets[*requested]
	if !ok || subnet.NetworkID != network.ID || subnet.ProjectID != network.ProjectID {
		return domain.Subnet{}, ErrPortInvalidSubnet
	}
	return subnet, nil
}

func firstFreeAddress(subnet domain.Subnet, used map[netip.Addr]bool) (netip.Addr, error) {
	if subnet.ID == "" {
		return netip.Addr{}, nil
	}
	// Each iteration skips an allocated address, so sparse pools avoid a full scan.
	address := subnet.AllocationStart
	for address.Compare(subnet.AllocationEnd) <= 0 {
		if !used[address] {
			return address, nil
		}
		address = address.Next()
	}
	return netip.Addr{}, ErrPortIPExhausted
}

func portMAC(id string) string {
	digest := sha256.Sum256([]byte(id))
	return net.HardwareAddr{0x02, digest[0], digest[1], digest[2], digest[3], digest[4]}.String()
}
