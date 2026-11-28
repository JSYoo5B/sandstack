package compute_test

import (
	"net/netip"

	app "github.com/JSYoo5B/sandstack/internal/app/compute"
	appnetwork "github.com/JSYoo5B/sandstack/internal/app/network"
	domain "github.com/JSYoo5B/sandstack/internal/domain/compute"
	networkdomain "github.com/JSYoo5B/sandstack/internal/domain/network"
)

func (s *ServersSuite) bootWithTwoPorts(name string) string {
	s.addSubnet(serverNetwork, "10.20.0.0/24")
	created, err := s.servers.Create(s.request("req-"+name), app.ServerCreate{
		Name: name, ImageID: serverImage, FlavorID: app.FixtureFlavorID,
		Networks: []app.NetworkRequest{{NetworkID: serverNetwork}, {NetworkID: serverNetwork}},
	})
	s.Require().NoError(err)
	return created.Server.ID
}

func (s *ServersSuite) portGroups(serverID string) [][]string {
	detail, err := s.servers.Get(serverProject, serverID)
	s.Require().NoError(err)
	var groups [][]string
	for _, serverPort := range detail.Ports {
		port, err := s.network.GetPort(serverProject, serverPort.ID)
		s.Require().NoError(err)
		groups = append(groups, port.SecurityGroups)
	}
	return groups
}

func (s *ServersSuite) defaultGroupID() string {
	for _, group := range s.network.ProjectSecurityGroups(serverProject) {
		if group.Name == "default" {
			return group.ID
		}
	}
	s.FailNow("the default security group is missing")
	return ""
}

// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/network/security_group_api.py#L587-L709
// https://github.com/openstack/python-neutronclient/blob/11.6.0/neutronclient/v2_0/client.py#L418-L492
func (s *ServersSuite) TestAddAndRemoveSecurityGroupOnEveryPort() {
	serverID := s.bootWithTwoPorts("web")
	defaultID := s.defaultGroupID()
	web, err := s.network.CreateSecurityGroup(serverProject, "web", "Web access", true)
	s.Require().NoError(err)
	s.Require().NoError(s.compute.Lock(s.request("req-lock"), serverID))

	addErr := s.servers.AddSecurityGroup(s.request("req-add"), serverID, "web")

	s.Require().NoError(addErr)
	s.Equal([][]string{{defaultID, web.ID}, {defaultID, web.ID}}, s.portGroups(serverID))
	detail, err := s.servers.Get(serverProject, serverID)
	s.Require().NoError(err)
	s.Equal([]string{"default", "web", "default", "web"}, detail.SecurityGroups)
	s.Equal([]string{domain.ActionLock, domain.ActionCreate}, s.actionNames(serverID))

	removeErr := s.servers.RemoveSecurityGroup(s.request("req-remove"), serverID, web.ID)

	s.Require().NoError(removeErr)
	s.Equal([][]string{{defaultID}, {defaultID}}, s.portGroups(serverID))
}

// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/security_groups.py#L46-L135
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/security_groups.py#L364-L398
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/network/security_group_api.py#L257-L290
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/network/security_group_api.py#L521-L575
func (s *ServersSuite) TestListSecurityGroupsInNovaFormat() {
	serverID := s.bootWithTwoPorts("web")
	defaultID := s.defaultGroupID()
	web, err := s.network.CreateSecurityGroup(serverProject, "web", "Web access", true)
	s.Require().NoError(err)
	ssh, err := s.network.CreateSecurityGroupRule(serverProject, web.ID, networkdomain.SecurityGroupRule{
		Direction: "ingress", EtherType: "IPv4", Protocol: "tcp",
		RemoteIPPrefix: netip.MustParsePrefix("192.0.2.0/24"),
	})
	s.Require().NoError(err)
	ping, err := s.network.CreateSecurityGroupRule(serverProject, web.ID, networkdomain.SecurityGroupRule{
		Direction: "ingress", EtherType: "IPv4", Protocol: "icmp",
	})
	s.Require().NoError(err)
	s.Require().NoError(s.servers.AddSecurityGroup(s.request("req-add"), serverID, "web"))

	groups, err := s.servers.ListSecurityGroups(serverProject, serverID)

	s.Require().NoError(err)
	self := &app.SecurityGroupReference{Name: "default", ProjectID: serverProject}
	defaultGroup := app.ServerSecurityGroup{
		ID: defaultID, Name: "default", Description: "Default security group", ProjectID: serverProject,
		Rules: []app.ServerSecurityGroupRule{
			{ID: "33aa02ab-8045-4e87-a724-491be582c6fe", ParentGroupID: defaultID, RemoteGroup: self},
			{ID: "4025f933-de76-4940-aeaf-70248b39f59c", ParentGroupID: defaultID, RemoteGroup: self},
		},
	}
	tcp, icmp := "tcp", "icmp"
	low, high, none := 1, 65535, -1
	webGroup := app.ServerSecurityGroup{
		ID: web.ID, Name: "web", Description: "Web access", ProjectID: serverProject,
		Rules: []app.ServerSecurityGroupRule{
			{
				ID: ssh.Rule.ID, ParentGroupID: web.ID, Protocol: &tcp,
				FromPort: &low, ToPort: &high, CIDR: "192.0.2.0/24",
			},
			{
				ID: ping.Rule.ID, ParentGroupID: web.ID, Protocol: &icmp,
				FromPort: &none, ToPort: &none, CIDR: "0.0.0.0/0",
			},
		},
	}
	s.Equal([]app.ServerSecurityGroup{defaultGroup, defaultGroup, webGroup, webGroup}, groups)
}

// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/security_groups.py#L407-L468
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/network/security_group_api.py#L578-L709
// https://github.com/openstack/neutron-lib/blob/5.0.0/neutron_lib/api/validators/__init__.py#L194-L209
func (s *ServersSuite) TestSecurityGroupActionsRejectInOrder() {
	serverID := s.bootWithTwoPorts("web")
	for _, name := range []string{"twin", "twin"} {
		_, err := s.network.CreateSecurityGroup(serverProject, name, "", true)
		s.Require().NoError(err)
	}
	stateless, err := s.network.CreateSecurityGroup(serverProject, "stateless", "", false)
	s.Require().NoError(err)
	request := s.request("req-add")
	cases := []struct {
		name    string
		call    func() error
		kind    error
		message string
	}{
		{
			name:    "unknown server",
			call:    func() error { return s.servers.AddSecurityGroup(request, "missing", "  ") },
			kind:    app.ErrServerNotFound,
			message: "compute server not found",
		},
		{
			name:    "blank name",
			call:    func() error { return s.servers.AddSecurityGroup(request, serverID, "  ") },
			kind:    app.ErrSecurityGroupNameEmpty,
			message: "Security group name cannot be empty",
		},
		{
			name:    "unknown group",
			call:    func() error { return s.servers.AddSecurityGroup(request, serverID, "absent") },
			kind:    app.ErrSecurityGroupNotFound,
			message: "Security group absent is not found for project " + serverProject,
		},
		{
			name: "ambiguous name",
			call: func() error { return s.servers.RemoveSecurityGroup(request, serverID, "twin") },
			kind: app.ErrSecurityGroupAmbiguous,
			message: "Multiple security_group matches found for name 'twin', " +
				"use an ID to be more specific.",
		},
		{
			name:    "server without ports",
			call:    func() error { return s.servers.AddSecurityGroup(request, "fixture", "default") },
			kind:    app.ErrSecurityGroupNotFound,
			message: "instance_id fixture could not be found as device id on any ports",
		},
		{
			name: "group already present",
			call: func() error { return s.servers.AddSecurityGroup(request, serverID, "default") },
			kind: app.ErrSecurityGroupCannotBeApplied,
			message: "Invalid input for operation: Invalid input for security_groups. " +
				"Reason: Duplicate items in the list: '" + s.defaultGroupID() + "'..",
		},
		{
			name:    "mixed connection state",
			call:    func() error { return s.servers.AddSecurityGroup(request, serverID, stateless.ID) },
			kind:    app.ErrSecurityGroupStateConflict,
			message: networkdomain.ErrSecurityGroupConflict.Error(),
		},
		{
			name:    "group not associated",
			call:    func() error { return s.servers.RemoveSecurityGroup(request, serverID, "stateless") },
			kind:    app.ErrSecurityGroupNotFound,
			message: "Security group stateless not associated with the instance " + serverID,
		},
	}
	defaultID := s.defaultGroupID()

	for _, tc := range cases {
		err := tc.call()

		s.ErrorIs(err, tc.kind, tc.name)
		s.EqualError(err, tc.message, tc.name)
	}
	s.Equal([][]string{{defaultID}, {defaultID}}, s.portGroups(serverID))
}

// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/network/security_group_api.py#L578-L584
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/exception.py#L1145-L1147
func (s *ServersSuite) TestAddSecurityGroupRequiresPortSecurity() {
	serverID := s.bootWithTwoPorts("web")
	detail, err := s.servers.Get(serverProject, serverID)
	s.Require().NoError(err)
	disabled, cleared := false, []string{}
	_, err = s.network.UpdatePort(serverProject, detail.Ports[0].ID, appnetwork.PortUpdate{
		SecurityGroups: &cleared, PortSecurityEnabled: &disabled,
	})
	s.Require().NoError(err)

	_, err = s.network.CreateSecurityGroup(serverProject, "web", "", true)
	s.Require().NoError(err)

	addErr := s.servers.AddSecurityGroup(s.request("req-add"), serverID, "web")

	s.ErrorIs(addErr, app.ErrSecurityGroupCannotBeApplied)
	s.EqualError(addErr,
		"Network requires port_security_enabled and subnet associated in order to apply security groups.")
	s.Equal([][]string{{}, {s.defaultGroupID()}}, s.portGroups(serverID))
}
