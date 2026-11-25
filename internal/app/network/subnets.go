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

type SubnetCreate struct {
	Name       string
	CIDR       netip.Prefix
	EnableDHCP bool
	// A nil Gateway selects the default. A pointer to an invalid address omits the gateway.
	Gateway         *netip.Addr
	DNSNameservers  []netip.Addr
	HostRoutes      []domain.SubnetHostRoute
	AllocationPools []domain.AllocationPool
}

func (s *Service) CreateSubnet(projectID, networkID string, options SubnetCreate) (domain.Subnet, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	network, ok := s.networks[networkID]
	if !ok || network.ProjectID != projectID {
		return domain.Subnet{}, ErrNetworkNotFound
	}
	prefix := options.CIDR
	if !prefix.Addr().Is4() || prefix.Bits() < 1 || prefix.Bits() > 30 {
		return domain.Subnet{}, ErrSubnetCIDR
	}
	prefix = prefix.Masked()
	for _, subnetID := range network.SubnetIDs {
		if s.subnets[subnetID].CIDR.Overlaps(prefix) {
			return domain.Subnet{}, ErrSubnetOverlap
		}
	}

	gateway, err := subnetGateway(prefix, options.Gateway)
	if err != nil {
		return domain.Subnet{}, err
	}
	pools, err := subnetAllocationPools(prefix, gateway, options.AllocationPools)
	if err != nil {
		return domain.Subnet{}, err
	}
	id, err := s.allocateID(nil)
	if err != nil {
		return domain.Subnet{}, err
	}
	subnet := domain.Subnet{
		ID:              id,
		NetworkID:       networkID,
		ProjectID:       network.ProjectID,
		Name:            options.Name,
		CIDR:            prefix,
		EnableDHCP:      options.EnableDHCP,
		DNSNameservers:  slices.Clone(options.DNSNameservers),
		HostRoutes:      slices.Clone(options.HostRoutes),
		Gateway:         gateway,
		AllocationPools: pools,
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
	Gateway        *netip.Addr
	DNSNameservers []netip.Addr
	HostRoutes     []domain.SubnetHostRoute
}

func (s *Service) UpdateSubnet(projectID, subnetID string, changes SubnetUpdate) (domain.Subnet, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	subnet, ok := s.subnets[subnetID]
	if !ok || subnet.ProjectID != projectID {
		return domain.Subnet{}, ErrSubnetNotFound
	}
	if changes.Gateway != nil {
		gateway, err := s.changeSubnetGateway(subnet, changes.Gateway)
		if err != nil {
			return domain.Subnet{}, err
		}
		subnet.Gateway = gateway
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
	if changes.HostRoutes != nil {
		subnet.HostRoutes = slices.Clone(changes.HostRoutes)
	}
	s.subnets[subnetID] = subnet
	return copySubnet(subnet), nil
}

type SubnetListFilter struct {
	Names      []string
	NetworkIDs []string
	CIDRs      []netip.Prefix
	DHCPStates []bool
}

func (f SubnetListFilter) matches(subnet domain.Subnet) bool {
	return (len(f.Names) == 0 || slices.Contains(f.Names, subnet.Name)) &&
		(len(f.NetworkIDs) == 0 || slices.Contains(f.NetworkIDs, subnet.NetworkID)) &&
		(len(f.CIDRs) == 0 || slices.Contains(f.CIDRs, subnet.CIDR)) &&
		(len(f.DHCPStates) == 0 || slices.Contains(f.DHCPStates, subnet.EnableDHCP))
}

// ListSubnets returns the project's matching subnets ordered by ID.
func (s *Service) ListSubnets(projectID string, filter SubnetListFilter) []domain.Subnet {
	subnets := []domain.Subnet{}
	s.mu.RLock()
	for _, subnet := range s.subnets {
		if subnet.ProjectID == projectID && filter.matches(subnet) {
			subnets = append(subnets, copySubnet(subnet))
		}
	}
	s.mu.RUnlock()

	slices.SortFunc(subnets, func(a, b domain.Subnet) int { return strings.Compare(a.ID, b.ID) })
	return subnets
}

func copySubnet(subnet domain.Subnet) domain.Subnet {
	subnet.DNSNameservers = slices.Clone(subnet.DNSNameservers)
	subnet.HostRoutes = slices.Clone(subnet.HostRoutes)
	subnet.AllocationPools = slices.Clone(subnet.AllocationPools)
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
