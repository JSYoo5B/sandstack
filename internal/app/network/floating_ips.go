package network

import (
	"errors"
	"net/netip"
	"slices"
	"strings"

	domain "github.com/JSYoo5B/sandstack/internal/domain/network"
)

var (
	ErrFloatingIPNotFound                   = errors.New("floating IP not found")
	ErrFloatingIPNoIPv4Subnet               = errors.New("network does not contain any IPv4 subnet")
	ErrFloatingIPFixedIPWithoutPort         = errors.New("fixed_ip_address cannot be specified without a port_id")
	ErrFloatingIPPortNoIPv4                 = errors.New("cannot add a floating IP to a port without a fixed IPv4 address")
	ErrFloatingIPFixedIPNotOnPort           = errors.New("the port does not have the requested fixed IPv4 address")
	ErrExternalGatewayForFloatingIPNotFound = errors.New("external network is not reachable from the port's subnet")
	ErrFloatingIPPortAlreadyAssociated      = errors.New("the fixed IP already has a floating IP on the external network")
	ErrRouterGatewayInUseByFloatingIP       = errors.New("router gateway is required by floating IPs")
	ErrRouterInterfaceInUseByFloatingIP     = errors.New("router interface is required by floating IPs")
	ErrPortFixedIPHasFloatingIP             = errors.New("the port's fixed IP is associated with a floating IP")
)

type FloatingIPCreate struct {
	NetworkID   string
	Description string
	PortID      string
	FixedIP     netip.Addr
}

type FloatingIPUpdate struct {
	Description *string
	PortID      *string
	FixedIP     netip.Addr
}

func (s *Service) CreateFloatingIP(projectID string, request FloatingIPCreate) (domain.FloatingIP, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	network, ok := s.networks[request.NetworkID]
	if !ok || !visibleNetwork(network, projectID) {
		return domain.FloatingIP{}, &NetworkNotFoundError{ID: request.NetworkID}
	}
	if !network.External {
		return domain.FloatingIP{}, ErrNetworkNotExternal
	}
	if len(network.SubnetIDs) == 0 {
		return domain.FloatingIP{}, ErrFloatingIPNoIPv4Subnet
	}
	subnet := s.subnets[network.SubnetIDs[0]]
	usedMACs := make(map[string]bool)
	usedIPs := make(map[netip.Addr]bool)
	for _, port := range s.ports {
		if port.NetworkID == network.ID {
			usedMACs[port.MACAddress] = true
		}
		if port.SubnetID == subnet.ID {
			usedIPs[port.FixedIP] = true
		}
	}
	address, err := selectPortAddress(subnet, nil, usedIPs)
	if err != nil {
		return domain.FloatingIP{}, err
	}

	floatingIP := domain.FloatingIP{
		ProjectID:         projectID,
		Description:       request.Description,
		FloatingNetworkID: network.ID,
		FloatingAddress:   address,
		Status:            domain.FloatingIPStatusDown,
	}
	portID := request.PortID
	if err := s.associateFloatingIP(&floatingIP, &portID, request.FixedIP); err != nil {
		return domain.FloatingIP{}, err
	}
	id, err := s.allocateID(nil)
	if err != nil {
		return domain.FloatingIP{}, err
	}
	floatingIP.ID = id
	s.floatingIPs[id] = floatingIP
	port, err := s.createPort(domain.Port{
		NetworkID:            network.ID,
		DeviceID:             id,
		DeviceOwner:          domain.DeviceOwnerFloatingIP,
		SubnetID:             subnet.ID,
		FixedIP:              address,
		PortSecurityDisabled: true,
	}, usedMACs)
	if err != nil {
		delete(s.floatingIPs, id)
		return domain.FloatingIP{}, err
	}
	floatingIP.FloatingPortID = port.ID
	s.storeFloatingIP(floatingIP)
	return floatingIP, nil
}

func (s *Service) GetFloatingIP(projectID, floatingIPID string) (domain.FloatingIP, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	floatingIP, ok := s.floatingIPs[floatingIPID]
	if !ok || floatingIP.ProjectID != projectID {
		return domain.FloatingIP{}, ErrFloatingIPNotFound
	}
	return floatingIP, nil
}

func (s *Service) UpdateFloatingIP(
	projectID, floatingIPID string,
	changes FloatingIPUpdate,
) (domain.FloatingIP, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	floatingIP, ok := s.floatingIPs[floatingIPID]
	if !ok || floatingIP.ProjectID != projectID {
		return domain.FloatingIP{}, ErrFloatingIPNotFound
	}
	if err := s.associateFloatingIP(&floatingIP, changes.PortID, changes.FixedIP); err != nil {
		return domain.FloatingIP{}, err
	}
	if changes.Description != nil {
		floatingIP.Description = *changes.Description
	}
	s.storeFloatingIP(floatingIP)
	return floatingIP, nil
}

func (s *Service) DeleteFloatingIP(projectID, floatingIPID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	floatingIP, ok := s.floatingIPs[floatingIPID]
	if !ok || floatingIP.ProjectID != projectID {
		return ErrFloatingIPNotFound
	}
	delete(s.ports, floatingIP.FloatingPortID)
	delete(s.floatingIPs, floatingIPID)
	return nil
}

type FloatingIPListFilter struct {
	IDs               []string
	NetworkIDs        []string
	PortIDs           []string
	RouterIDs         []string
	Statuses          []string
	FixedAddresses    []string
	FloatingAddresses []string
	Tags              TagFilter
}

func (f FloatingIPListFilter) matches(floatingIP domain.FloatingIP) bool {
	return (len(f.IDs) == 0 || slices.Contains(f.IDs, floatingIP.ID)) &&
		(len(f.NetworkIDs) == 0 || slices.Contains(f.NetworkIDs, floatingIP.FloatingNetworkID)) &&
		(len(f.PortIDs) == 0 || (floatingIP.PortID != "" && slices.Contains(f.PortIDs, floatingIP.PortID))) &&
		(len(f.RouterIDs) == 0 || (floatingIP.RouterID != "" && slices.Contains(f.RouterIDs, floatingIP.RouterID))) &&
		(len(f.Statuses) == 0 || slices.Contains(f.Statuses, floatingIP.Status)) &&
		(len(f.FixedAddresses) == 0 ||
			(floatingIP.FixedIP.IsValid() && slices.Contains(f.FixedAddresses, floatingIP.FixedIP.String()))) &&
		(len(f.FloatingAddresses) == 0 || slices.Contains(f.FloatingAddresses, floatingIP.FloatingAddress.String())) &&
		f.Tags.matches(floatingIP.Tags)
}

func (s *Service) ListFloatingIPs(projectID string, filter FloatingIPListFilter) []domain.FloatingIP {
	page, _ := s.ListFloatingIPsPage(projectID, filter, PageOptions{})
	return page.Items
}

func (s *Service) ListFloatingIPsPage(
	projectID string,
	filter FloatingIPListFilter,
	options PageOptions,
) (Page[domain.FloatingIP], error) {
	floatingIPs := []domain.FloatingIP{}
	var marker *domain.FloatingIP
	s.mu.RLock()
	if options.Limit > 0 && options.Marker != "" {
		floatingIP, exists := s.floatingIPs[options.Marker]
		if !exists || floatingIP.ProjectID != projectID {
			s.mu.RUnlock()
			return Page[domain.FloatingIP]{}, ErrFloatingIPNotFound
		}
		marker = &floatingIP
	}
	for _, floatingIP := range s.floatingIPs {
		if floatingIP.ProjectID == projectID && filter.matches(floatingIP) {
			floatingIPs = append(floatingIPs, floatingIP)
		}
	}
	s.mu.RUnlock()
	return paginate(floatingIPs, options, marker,
		func(a, b domain.FloatingIP) int { return strings.Compare(a.ID, b.ID) },
		func(floatingIP domain.FloatingIP) string { return floatingIP.ID },
	), nil
}

func (s *Service) associateFloatingIP(floatingIP *domain.FloatingIP, portID *string, fixedIP netip.Addr) error {
	if fixedIP.IsValid() && (portID == nil || *portID == "") {
		return ErrFloatingIPFixedIPWithoutPort
	}
	target := floatingIP.PortID
	if portID != nil {
		target = *portID
	}
	if target == "" {
		floatingIP.PortID, floatingIP.FixedIP, floatingIP.RouterID = "", netip.Addr{}, ""
		return nil
	}

	port, ok := s.ports[target]
	if !ok || port.ProjectID != floatingIP.ProjectID {
		return ErrPortNotFound
	}
	if fixedIP.IsValid() {
		if !fixedIP.Is4() || port.FixedIP != fixedIP {
			return ErrFloatingIPFixedIPNotOnPort
		}
	} else if !port.FixedIP.Is4() {
		return ErrFloatingIPPortNoIPv4
	}
	routerID, err := s.floatingIPRouter(port, floatingIP.FloatingNetworkID)
	if err != nil {
		return err
	}
	if target != floatingIP.PortID {
		for _, other := range s.floatingIPs {
			if other.ID != floatingIP.ID && other.PortID == target &&
				other.FloatingNetworkID == floatingIP.FloatingNetworkID && other.FixedIP == port.FixedIP {
				return ErrFloatingIPPortAlreadyAssociated
			}
		}
	}
	floatingIP.PortID, floatingIP.FixedIP, floatingIP.RouterID = target, port.FixedIP, routerID
	return nil
}

func (s *Service) floatingIPRouter(port domain.Port, externalNetworkID string) (string, error) {
	subnet := s.subnets[port.SubnetID]
	var candidates []string
	for _, routerPort := range s.ports {
		if routerPort.DeviceOwner != domain.DeviceOwnerRouterInterface ||
			routerPort.NetworkID != port.NetworkID || routerPort.SubnetID != port.SubnetID {
			continue
		}
		router, ok := s.routers[routerPort.DeviceID]
		if !ok || router.Gateway == nil || router.Gateway.NetworkID != externalNetworkID {
			continue
		}
		if subnet.Gateway.IsValid() && routerPort.FixedIP == subnet.Gateway {
			return router.ID, nil
		}
		candidates = append(candidates, router.ID)
	}
	if len(candidates) == 0 {
		return "", ErrExternalGatewayForFloatingIPNotFound
	}
	return slices.Min(candidates), nil
}

func (s *Service) storeFloatingIP(floatingIP domain.FloatingIP) {
	floatingIP.Status = domain.FloatingIPStatusDown
	if floatingIP.PortID != "" {
		floatingIP.Status = domain.FloatingIPStatusActive
	}
	s.floatingIPs[floatingIP.ID] = floatingIP
}

func (s *Service) disassociatePortFloatingIPs(portID string) {
	for _, floatingIP := range s.floatingIPs {
		if floatingIP.PortID == portID {
			floatingIP.PortID, floatingIP.FixedIP, floatingIP.RouterID = "", netip.Addr{}, ""
			s.storeFloatingIP(floatingIP)
		}
	}
}

func (s *Service) routerGatewayServesFloatingIPs(routerID string) bool {
	for _, floatingIP := range s.floatingIPs {
		if floatingIP.RouterID == routerID {
			return true
		}
	}
	return false
}

func (s *Service) routerInterfaceServesFloatingIPs(routerID, subnetID string) bool {
	for _, floatingIP := range s.floatingIPs {
		if floatingIP.RouterID == routerID && s.ports[floatingIP.PortID].SubnetID == subnetID {
			return true
		}
	}
	return false
}

func (s *Service) portFloatingIPAddress(portID string) (netip.Addr, bool) {
	for _, floatingIP := range s.floatingIPs {
		if floatingIP.PortID == portID {
			return floatingIP.FixedIP, true
		}
	}
	return netip.Addr{}, false
}
