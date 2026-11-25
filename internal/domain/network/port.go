package network

import "net/netip"

type Port struct {
	ID         string
	NetworkID  string
	ProjectID  string
	Name       string
	MACAddress string
	SubnetID   string
	FixedIP    netip.Addr
	AdminDown  bool
}
