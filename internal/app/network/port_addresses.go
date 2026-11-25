package network

import (
	"crypto/sha256"
	"net"
	"net/netip"

	domain "github.com/JSYoo5B/sandstack/internal/domain/network"
)

func (s *Service) selectPortSubnet(
	network domain.Network, requested *string, address *netip.Addr,
) (domain.Subnet, error) {
	if address == nil {
		return s.portSubnet(network, requested)
	}
	return s.portFixedIPSubnet(network, requested, *address)
}

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

func (s *Service) portFixedIPSubnet(
	network domain.Network, requested *string, address netip.Addr,
) (domain.Subnet, error) {
	if requested != nil {
		subnet, err := s.portSubnet(network, requested)
		if err != nil || subnet.ID == "" {
			return domain.Subnet{}, ErrPortInvalidSubnet
		}
		if !isUsableIPv4(subnet, address) {
			return domain.Subnet{}, ErrPortInvalidIPForSubnet
		}
		return subnet, nil
	}
	for _, id := range network.SubnetIDs {
		subnet := s.subnets[id]
		if isUsableIPv4(subnet, address) {
			return subnet, nil
		}
	}
	return domain.Subnet{}, ErrPortInvalidIPForNetwork
}

func isUsableIPv4(subnet domain.Subnet, address netip.Addr) bool {
	return address.Is4() && address.Compare(subnet.CIDR.Addr().Next()) >= 0 &&
		address.Compare(lastUsableAddress(subnet.CIDR)) <= 0
}

func selectPortAddress(subnet domain.Subnet, requested *netip.Addr, used map[netip.Addr]bool) (netip.Addr, error) {
	if requested != nil {
		if used[*requested] {
			return netip.Addr{}, ErrPortIPAlreadyAllocated
		}
		return *requested, nil
	}
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
