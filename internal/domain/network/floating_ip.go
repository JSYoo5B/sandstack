package network

import "net/netip"

const (
	DeviceOwnerFloatingIP = "network:floatingip"

	FloatingIPStatusActive = "ACTIVE"
	FloatingIPStatusDown   = "DOWN"
)

type FloatingIP struct {
	ID                string
	ProjectID         string
	Description       string
	FloatingNetworkID string
	FloatingAddress   netip.Addr
	FloatingPortID    string
	PortID            string
	FixedIP           netip.Addr
	RouterID          string
	Status            string
	Tags              []string
}
