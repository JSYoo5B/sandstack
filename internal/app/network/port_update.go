package network

import (
	"errors"
	"net/netip"

	domain "github.com/JSYoo5B/sandstack/internal/domain/network"
)

var ErrPortSecurityRequired = errors.New("port security is required to assign security groups")

type PortUpdate struct {
	Name                *string
	AdminStateUp        *bool
	FixedIPs            *PortFixedIPUpdate
	SecurityGroups      *[]string
	PortSecurityEnabled *bool
	AllowedAddressPairs *[]domain.AddressPair
}

// PortFixedIPUpdate replaces the port's address. Its zero value clears it.
type PortFixedIPUpdate struct {
	SubnetID *string
	Address  netip.Addr
}

func (s *Service) UpdatePort(projectID, portID string, changes PortUpdate) (domain.Port, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	port, ok := s.ports[portID]
	if !ok || port.ProjectID != projectID {
		return domain.Port{}, ErrPortNotFound
	}
	if changes.FixedIPs != nil {
		if port.DeviceOwner == domain.DeviceOwnerRouterInterface {
			return domain.Port{}, ErrServicePortInUse
		}
		subnetID, address, err := s.updatedPortAddress(port, *changes.FixedIPs)
		if err != nil {
			return domain.Port{}, err
		}
		if floatingFixedIP, ok := s.portFloatingIPAddress(portID); ok && address != floatingFixedIP {
			return domain.Port{}, ErrPortFixedIPHasFloatingIP
		}
		port.SubnetID = subnetID
		port.FixedIP = address
	}
	if err := checkUpdatePortSecurity(port, changes); err != nil {
		return domain.Port{}, err
	}
	if changes.AllowedAddressPairs != nil {
		pairs, err := resolveAddressPairs(*changes.AllowedAddressPairs, port.MACAddress)
		if err != nil {
			return domain.Port{}, err
		}
		port.AllowedAddressPairs = pairs
	}
	if changes.SecurityGroups != nil {
		groupIDs, err := s.portSecurityGroups(port.ProjectID, changes.SecurityGroups)
		if err != nil {
			return domain.Port{}, err
		}
		port.SecurityGroups = groupIDs
	}
	if changes.Name != nil {
		port.Name = *changes.Name
	}
	if changes.AdminStateUp != nil {
		port.AdminDown = !*changes.AdminStateUp
	}
	if changes.PortSecurityEnabled != nil {
		port.PortSecurityDisabled = !*changes.PortSecurityEnabled
	}
	s.ports[portID] = port
	return clonePort(port), nil
}

// updatedPortAddress resolves a replacement while the write lock is held.
func (s *Service) updatedPortAddress(port domain.Port, change PortFixedIPUpdate) (string, netip.Addr, error) {
	var requested *netip.Addr
	if change.Address.IsValid() {
		requested = &change.Address
	}
	if change.SubnetID == nil && requested == nil {
		return "", netip.Addr{}, nil
	}
	if change.SubnetID != nil && *change.SubnetID == "" {
		return "", netip.Addr{}, ErrPortInvalidSubnet
	}

	subnet, err := s.selectPortSubnet(s.networks[port.NetworkID], change.SubnetID, requested)
	if err != nil {
		return "", netip.Addr{}, err
	}
	if requested == nil && subnet.ID == port.SubnetID && port.FixedIP.Is4() {
		return subnet.ID, port.FixedIP, nil
	}

	used := make(map[netip.Addr]bool)
	for _, other := range s.ports {
		if other.ID != port.ID && other.SubnetID == subnet.ID {
			used[other.FixedIP] = true
		}
	}
	address, err := selectPortAddress(subnet, requested, used)
	return subnet.ID, address, err
}
