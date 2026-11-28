package network

import (
	"errors"
	"net/netip"
)

var (
	ErrPortIPExhausted        = errors.New("port subnet allocation pool is exhausted")
	ErrPortIPAlreadyAllocated = errors.New("port address is already allocated on the subnet")
)

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
	AllowedAddressPairs  []AddressPair
	Tags                 []string
}

func (p Port) Status() string { return "DOWN" }

type AddressPair struct {
	IPAddress  string
	MACAddress string
}

// DeviceInterface describes a port as the device it is bound to reports it.
type DeviceInterface struct {
	Port               Port
	NetworkName        string
	FloatingAddresses  []netip.Addr
	SecurityGroupNames []string
}
