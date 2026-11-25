package network

import "net/netip"

type Subnet struct {
	ID              string
	NetworkID       string
	ProjectID       string
	Name            string
	CIDR            netip.Prefix
	EnableDHCP      bool
	Gateway         netip.Addr
	AllocationStart netip.Addr
	AllocationEnd   netip.Addr
}
