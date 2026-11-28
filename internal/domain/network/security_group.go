package network

import (
	"errors"
	"net/netip"
)

var ErrSecurityGroupConflict = errors.New(
	"cannot apply both stateful and stateless security groups on the same port")

type SecurityGroup struct {
	ID          string
	Name        string
	Description string
	ProjectID   string
	Stateful    bool
	Rules       []SecurityGroupRule
	Tags        []string
}

type SecurityGroupRule struct {
	ID             string
	Description    string
	Direction      string
	EtherType      string
	Protocol       string
	PortRangeMin   *int
	PortRangeMax   *int
	RemoteGroupID  string
	RemoteIPPrefix netip.Prefix
}
