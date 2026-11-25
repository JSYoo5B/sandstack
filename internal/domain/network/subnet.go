package network

import "net/netip"

type Subnet struct {
	ID              string
	NetworkID       string
	ProjectID       string
	Name            string
	CIDR            netip.Prefix
	EnableDHCP      bool
	DNSNameservers  []netip.Addr
	HostRoutes      []SubnetHostRoute
	Gateway         netip.Addr
	AllocationStart netip.Addr
	AllocationEnd   netip.Addr
}

type SubnetHostRoute struct {
	Destination netip.Prefix
	NextHop     netip.Addr
}
