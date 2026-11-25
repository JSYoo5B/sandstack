package network

import (
	"encoding/binary"
	"errors"
	"net/netip"
	"slices"
	"strings"

	domain "github.com/JSYoo5B/sandstack/internal/domain/network"
)

var (
	ErrSubnetNotFound = errors.New("subnet not found")
	ErrSubnetCIDR     = errors.New("subnet requires an IPv4 prefix from /1 through /30")
	ErrSubnetOverlap  = errors.New("subnet CIDR overlaps another subnet on the network")
	ErrSubnetInUse    = errors.New("subnet has allocated port addresses")
)

func (s *Service) CreateSubnet(
	projectID, networkID, name string,
	prefix netip.Prefix,
	enableDHCP bool,
	dnsNameservers ...netip.Addr,
) (domain.Subnet, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	network, ok := s.networks[networkID]
	if !ok || network.ProjectID != projectID {
		return domain.Subnet{}, ErrNetworkNotFound
	}
	if !prefix.Addr().Is4() || prefix.Bits() < 1 || prefix.Bits() > 30 {
		return domain.Subnet{}, ErrSubnetCIDR
	}
	prefix = prefix.Masked()
	for _, subnetID := range network.SubnetIDs {
		if s.subnets[subnetID].CIDR.Overlaps(prefix) {
			return domain.Subnet{}, ErrSubnetOverlap
		}
	}

	id, err := s.allocateID(nil)
	if err != nil {
		return domain.Subnet{}, err
	}
	gateway := prefix.Addr().Next()
	subnet := domain.Subnet{
		ID:              id,
		NetworkID:       networkID,
		ProjectID:       network.ProjectID,
		Name:            name,
		CIDR:            prefix,
		EnableDHCP:      enableDHCP,
		DNSNameservers:  slices.Clone(dnsNameservers),
		Gateway:         gateway,
		AllocationStart: gateway.Next(),
		AllocationEnd:   lastUsableAddress(prefix),
	}
	s.subnets[id] = subnet
	network.SubnetIDs = append(network.SubnetIDs, id)
	slices.Sort(network.SubnetIDs)
	s.networks[networkID] = network
	return copySubnet(subnet), nil
}

func lastUsableAddress(prefix netip.Prefix) netip.Addr {
	broadcast := prefix.Addr().As4()
	networkAddress := binary.BigEndian.Uint32(broadcast[:])
	hostMask := (uint32(1) << (32 - prefix.Bits())) - 1
	binary.BigEndian.PutUint32(broadcast[:], networkAddress|hostMask)
	return netip.AddrFrom4(broadcast).Prev()
}

func (s *Service) GetSubnet(projectID, subnetID string) (domain.Subnet, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	subnet, ok := s.subnets[subnetID]
	if !ok || subnet.ProjectID != projectID {
		return domain.Subnet{}, ErrSubnetNotFound
	}
	return copySubnet(subnet), nil
}

type SubnetUpdate struct {
	Name           *string
	EnableDHCP     *bool
	DNSNameservers []netip.Addr
}

func (s *Service) UpdateSubnet(projectID, subnetID string, changes SubnetUpdate) (domain.Subnet, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	subnet, ok := s.subnets[subnetID]
	if !ok || subnet.ProjectID != projectID {
		return domain.Subnet{}, ErrSubnetNotFound
	}
	if changes.Name != nil {
		subnet.Name = *changes.Name
	}
	if changes.EnableDHCP != nil {
		subnet.EnableDHCP = *changes.EnableDHCP
	}
	if changes.DNSNameservers != nil {
		subnet.DNSNameservers = slices.Clone(changes.DNSNameservers)
	}
	s.subnets[subnetID] = subnet
	return copySubnet(subnet), nil
}

// ListSubnets returns the project's subnets ordered by ID.
func (s *Service) ListSubnets(projectID string) []domain.Subnet {
	subnets := []domain.Subnet{}
	s.mu.RLock()
	for _, subnet := range s.subnets {
		if subnet.ProjectID == projectID {
			subnets = append(subnets, copySubnet(subnet))
		}
	}
	s.mu.RUnlock()

	slices.SortFunc(subnets, func(a, b domain.Subnet) int { return strings.Compare(a.ID, b.ID) })
	return subnets
}

func copySubnet(subnet domain.Subnet) domain.Subnet {
	subnet.DNSNameservers = slices.Clone(subnet.DNSNameservers)
	return subnet
}

func (s *Service) DeleteSubnet(projectID, subnetID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	subnet, ok := s.subnets[subnetID]
	if !ok || subnet.ProjectID != projectID {
		return ErrSubnetNotFound
	}
	for _, port := range s.ports {
		if port.SubnetID == subnetID {
			return ErrSubnetInUse
		}
	}
	network := s.networks[subnet.NetworkID]
	network.SubnetIDs = slices.DeleteFunc(network.SubnetIDs, func(id string) bool { return id == subnetID })
	s.networks[network.ID] = network
	delete(s.subnets, subnetID)
	return nil
}
