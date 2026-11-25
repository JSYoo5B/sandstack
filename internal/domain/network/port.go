package network

import "net/netip"

const DeviceOwnerRouterInterface = "network:router_interface"

type Port struct {
	ID             string
	NetworkID      string
	ProjectID      string
	Name           string
	MACAddress     string
	DeviceID       string
	DeviceOwner    string
	SubnetID       string
	FixedIP        netip.Addr
	AdminDown      bool
	SecurityGroups []string
}
