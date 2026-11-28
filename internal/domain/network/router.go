package network

import "net/netip"

type Router struct {
	ID           string
	ProjectID    string
	Name         string
	Description  string
	AdminStateUp bool
	Routes       []RouterRoute
	Gateway      *RouterGateway
}

type RouterGateway struct {
	NetworkID  string
	PortID     string
	SubnetID   string
	Address    netip.Addr
	EnableSNAT bool
}

type RouterRoute struct {
	Destination netip.Prefix
	NextHop     netip.Addr
}
