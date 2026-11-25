package network

import "net/netip"

type SecurityGroup struct {
	ID          string
	Name        string
	Description string
	ProjectID   string
	Stateful    bool
	Rules       []SecurityGroupRule
}

type SecurityGroupRule struct {
	ID             string
	Description    string
	Direction      string
	EtherType      string
	Protocol       string
	PortRangeMin   int
	PortRangeMax   int
	RemoteGroupID  string
	RemoteIPPrefix netip.Prefix
}
