package network

import "net/netip"

const (
	DeviceOwnerRouterInterface = "network:router_interface"
	DeviceOwnerRouterGateway   = "network:router_gateway"
	DeviceOwnerCompute         = "compute:nova"
)

type Port struct {
	ID                   string
	NetworkID            string
	ProjectID            string
	Name                 string
	MACAddress           string
	DeviceID             string
	DeviceOwner          string
	SubnetID             string
	FixedIP              netip.Addr
	AdminDown            bool
	SecurityGroups       []string
	PortSecurityDisabled bool
}

// DeviceInterface describes a port as the device it is bound to reports it.
type DeviceInterface struct {
	Port               Port
	NetworkName        string
	FloatingAddresses  []netip.Addr
	SecurityGroupNames []string
}
