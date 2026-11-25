package network

import "net/netip"

type Router struct {
	ID           string
	ProjectID    string
	Name         string
	Description  string
	AdminStateUp bool
	Routes       []RouterRoute
}

type RouterRoute struct {
	Destination netip.Prefix
	NextHop     netip.Addr
}
