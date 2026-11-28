package compute

import (
	"errors"
	"fmt"
	"net/netip"
	"slices"

	domain "github.com/JSYoo5B/sandstack/internal/domain/compute"
	networkdomain "github.com/JSYoo5B/sandstack/internal/domain/network"
)

var (
	ErrInvalidInterfaceRequest = errors.New("the interface attachment request is invalid")
	ErrInterfaceNotAttached    = errors.New("the port is not attached to the server")
	ErrExternalNetworkAttach   = errors.New("attaching to an external network is forbidden")
)

type InterfaceError struct {
	Kind    error
	Message string
}

func (e *InterfaceError) Error() string { return e.Message }

func (e *InterfaceError) Unwrap() error { return e.Kind }

func interfaceError(kind error, format string, args ...any) error {
	return &InterfaceError{Kind: kind, Message: fmt.Sprintf(format, args...)}
}

type InterfaceAttach struct {
	NetworkID string
	PortID    string
	FixedIP   *netip.Addr
}

type ServerInterface struct {
	PortID     string
	NetworkID  string
	MACAddress string
	PortState  string
	FixedIPs   []InterfaceFixedIP
}

type InterfaceFixedIP struct {
	SubnetID string
	Address  netip.Addr
}

var interfaceStates = []domain.Status{domain.StatusActive, domain.StatusPaused, domain.StatusShutoff}

func (s *Servers) ListInterfaces(projectID, serverID string) ([]ServerInterface, error) {
	if _, err := s.serverPortIDs(projectID, serverID); err != nil {
		return nil, err
	}
	ports := s.networks.DevicePorts(projectID, serverID)
	interfaces := make([]ServerInterface, len(ports))
	for i, port := range ports {
		interfaces[i] = describeInterface(port)
	}
	return interfaces, nil
}

func (s *Servers) GetInterface(projectID, serverID, portID string) (ServerInterface, error) {
	if _, err := s.serverPortIDs(projectID, serverID); err != nil {
		return ServerInterface{}, err
	}
	port, err := s.networks.GetPort(projectID, portID)
	if err != nil {
		return ServerInterface{}, interfaceError(ErrPortNotFound, "Port id %s could not be found.", portID)
	}
	if port.DeviceID != serverID {
		return ServerInterface{}, interfaceError(ErrInterfaceNotAttached,
			"Instance %s does not have a port with id %s", serverID, portID)
	}
	return describeInterface(port), nil
}

func (s *Servers) AttachInterface(
	request ActionRequest, serverID string, spec InterfaceAttach,
) (ServerInterface, error) {
	projectID := request.ProjectID
	s.compute.mu.Lock()
	defer s.compute.mu.Unlock()

	record, ok := s.compute.find(projectID, serverID)
	if !ok {
		return ServerInterface{}, ErrServerNotFound
	}
	if spec.NetworkID != "" && spec.PortID != "" {
		return ServerInterface{}, interfaceError(ErrInvalidInterfaceRequest,
			"Must not input both network_id and port_id")
	}
	if spec.FixedIP != nil && spec.NetworkID == "" {
		return ServerInterface{}, interfaceError(ErrInvalidInterfaceRequest,
			"Must input network_id when request IP address")
	}
	server := &record.server
	if server.Locked {
		return ServerInterface{}, &LockedError{ServerID: server.ID}
	}
	if err := checkInstanceState("attach_interface", *server, interfaceStates...); err != nil {
		return ServerInterface{}, err
	}
	s.compute.recordAction(record, request, domain.ActionAttachInterface)

	networkID, err := s.interfaceNetwork(projectID, spec)
	if err != nil {
		return ServerInterface{}, err
	}
	network, err := s.networks.Get(projectID, networkID)
	if err != nil {
		return ServerInterface{}, interfaceError(ErrNetworkNotFound, "Network %s could not be found.", networkID)
	}
	if network.External && network.ProjectID != projectID {
		return ServerInterface{}, interfaceError(ErrExternalNetworkAttach,
			"It is not allowed to create an interface on external network %s", network.ID)
	}
	if !network.PortSecurityDisabled && len(network.SubnetIDs) == 0 {
		return ServerInterface{}, interfaceError(ErrInvalidInterfaceRequest, securityGroupCannotBeApplied)
	}

	port, err := s.bindInterfacePort(projectID, serverID, network.ID, spec)
	if err != nil {
		return ServerInterface{}, err
	}
	server.Ports = append(server.Ports, domain.ServerPort{ID: port.ID, Preserve: spec.PortID != ""})
	return describeInterface(port), nil
}

func (s *Servers) interfaceNetwork(projectID string, spec InterfaceAttach) (string, error) {
	switch {
	case spec.PortID != "":
		port, err := s.networks.GetPort(projectID, spec.PortID)
		if err != nil {
			return "", interfaceError(ErrPortNotFound, "Port id %s could not be found.", spec.PortID)
		}
		if port.DeviceID != "" {
			return "", interfaceError(ErrPortInUse, "Port %s is still in use.", spec.PortID)
		}
		return port.NetworkID, nil
	case spec.NetworkID != "":
		return spec.NetworkID, nil
	}
	available := s.networks.AvailableNetworks(projectID)
	switch len(available) {
	case 0:
		return "", interfaceError(ErrInvalidInterfaceRequest,
			"No specific network was requested and none are available for project '%s'.", projectID)
	case 1:
		return available[0].ID, nil
	default:
		return "", interfaceError(ErrInvalidInterfaceRequest,
			"Multiple possible networks found, use a Network ID to be more specific.")
	}
}

func (s *Servers) bindInterfacePort(
	projectID, serverID, networkID string, spec InterfaceAttach,
) (networkdomain.Port, error) {
	if spec.PortID != "" {
		port, err := s.networks.BindPort(projectID, spec.PortID, serverID, networkdomain.DeviceOwnerCompute)
		if err != nil {
			return networkdomain.Port{}, interfaceError(ErrPortInUse, "Port %s is still in use.", spec.PortID)
		}
		return port, nil
	}
	port, err := s.networks.CreateDevicePort(
		projectID, networkID, spec.FixedIP, nil, serverID, networkdomain.DeviceOwnerCompute)
	switch {
	case errors.Is(err, networkdomain.ErrPortIPAlreadyAllocated):
		return networkdomain.Port{}, interfaceError(ErrFixedIPInUse, "Fixed IP %s is already in use.", spec.FixedIP)
	case errors.Is(err, networkdomain.ErrPortIPExhausted):
		return networkdomain.Port{}, interfaceError(ErrInvalidInterfaceRequest,
			"No fixed IP addresses available for network: %s", networkID)
	case err != nil:
		return networkdomain.Port{}, err
	}
	return port, nil
}

func (s *Servers) DetachInterface(request ActionRequest, serverID, portID string) error {
	projectID := request.ProjectID
	s.compute.mu.Lock()
	defer s.compute.mu.Unlock()

	record, ok := s.compute.find(projectID, serverID)
	if !ok {
		return ErrServerNotFound
	}
	server := &record.server
	if server.Locked {
		return &LockedError{ServerID: server.ID}
	}
	if err := checkInstanceState("detach_interface", *server, interfaceStates...); err != nil {
		return err
	}
	s.compute.recordAction(record, request, domain.ActionDetachInterface)

	index := slices.IndexFunc(server.Ports, func(port domain.ServerPort) bool { return port.ID == portID })
	if index < 0 {
		return nil
	}
	s.releasePorts(projectID, serverID, server.Ports[index:index+1])
	server.Ports = slices.Delete(slices.Clone(server.Ports), index, index+1)
	return nil
}

func describeInterface(port networkdomain.Port) ServerInterface {
	described := ServerInterface{
		PortID:     port.ID,
		NetworkID:  port.NetworkID,
		MACAddress: port.MACAddress,
		PortState:  port.Status(),
		FixedIPs:   []InterfaceFixedIP{},
	}
	if port.FixedIP.IsValid() {
		described.FixedIPs = append(described.FixedIPs, InterfaceFixedIP{SubnetID: port.SubnetID, Address: port.FixedIP})
	}
	return described
}
