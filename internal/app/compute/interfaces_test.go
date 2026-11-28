package compute_test

import (
	"net/netip"

	app "github.com/JSYoo5B/sandstack/internal/app/compute"
	appnetwork "github.com/JSYoo5B/sandstack/internal/app/network"
	domain "github.com/JSYoo5B/sandstack/internal/domain/compute"
	networkdomain "github.com/JSYoo5B/sandstack/internal/domain/network"
)

func (s *ServersSuite) bootOnDefaultNetwork(name string) string {
	s.addSubnet(serverNetwork, "10.20.0.0/24")
	created, err := s.servers.Create(s.request("req-"+name), app.ServerCreate{
		Name: name, ImageID: serverImage, FlavorID: app.FixtureFlavorID,
	})
	s.Require().NoError(err)
	return created.Server.ID
}

func (s *ServersSuite) interfacePortIDs(serverID string) []string {
	interfaces, err := s.servers.ListInterfaces(serverProject, serverID)
	s.Require().NoError(err)
	ids := make([]string, len(interfaces))
	for i, attached := range interfaces {
		ids[i] = attached.PortID
	}
	return ids
}

// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/attach_interfaces.py#L34-L54
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L5492-L5552
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/network/neutron.py#L472-L561
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/network/neutron.py#L1878-L1918
func (s *ServersSuite) TestAttachInterfaceByNetworkCreatesAndDeletesPort() {
	serverID := s.bootOnDefaultNetwork("web")
	address := netip.MustParseAddr("10.20.0.50")

	attached, err := s.servers.AttachInterface(s.request("req-attach"), serverID, app.InterfaceAttach{
		NetworkID: serverNetwork, FixedIP: &address,
	})

	s.Require().NoError(err)
	port, err := s.network.GetPort(serverProject, attached.PortID)
	s.Require().NoError(err)
	s.Equal(serverID, port.DeviceID)
	s.Equal(networkdomain.DeviceOwnerCompute, port.DeviceOwner)
	s.Equal(app.ServerInterface{
		PortID: port.ID, NetworkID: serverNetwork, MACAddress: port.MACAddress, PortState: "DOWN",
		FixedIPs: []app.InterfaceFixedIP{{SubnetID: port.SubnetID, Address: address}},
	}, attached)
	detail, err := s.servers.Get(serverProject, serverID)
	s.Require().NoError(err)
	s.Require().Len(detail.Ports, 2)
	s.Equal(domain.ServerPort{ID: port.ID}, detail.Ports[1])
	s.Require().Len(detail.Networks, 1)
	s.Equal([]app.ServerAddress{
		{Address: netip.MustParseAddr("10.20.0.2"), MAC: detail.Networks[0].Addresses[0].MAC},
		{Address: address, MAC: port.MACAddress},
	}, detail.Networks[0].Addresses)
	s.ElementsMatch([]string{detail.Ports[0].ID, port.ID}, s.interfacePortIDs(serverID))
	shown, err := s.servers.GetInterface(serverProject, serverID, port.ID)
	s.Require().NoError(err)
	s.Equal(attached, shown)

	detachErr := s.servers.DetachInterface(s.request("req-detach"), serverID, port.ID)

	s.Require().NoError(detachErr)
	_, err = s.network.GetPort(serverProject, port.ID)
	s.ErrorIs(err, appnetwork.ErrPortNotFound)
	detail, err = s.servers.Get(serverProject, serverID)
	s.Require().NoError(err)
	s.Len(detail.Ports, 1)
	s.Equal([]string{detail.Ports[0].ID}, s.interfacePortIDs(serverID))
	s.Equal([]string{domain.ActionDetachInterface, domain.ActionAttachInterface, domain.ActionCreate},
		s.actionNames(serverID))
}

// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/network/neutron.py#L713-L789
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/network/neutron.py#L611-L711
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/network/neutron.py#L1841-L1876
func (s *ServersSuite) TestAttachInterfaceByPortUnbindsOnDetachAndDelete() {
	serverID := s.bootOnDefaultNetwork("web")
	kept, err := s.network.CreatePort(serverProject, serverNetwork, "kept", false, nil)
	s.Require().NoError(err)
	stopped, err := s.network.CreatePort(serverProject, serverNetwork, "stopped", false, nil)
	s.Require().NoError(err)
	s.Require().NoError(s.compute.SetPower(s.request("req-stop"), serverID, false))

	attached, err := s.servers.AttachInterface(s.request("req-attach"), serverID, app.InterfaceAttach{
		PortID: kept.ID,
	})

	s.Require().NoError(err)
	s.Equal(kept.ID, attached.PortID)
	detail, err := s.servers.Get(serverProject, serverID)
	s.Require().NoError(err)
	s.Equal(domain.ServerPort{ID: kept.ID, Preserve: true}, detail.Ports[1])
	s.Require().NoError(s.servers.DetachInterface(s.request("req-detach"), serverID, kept.ID))
	port, err := s.network.GetPort(serverProject, kept.ID)
	s.Require().NoError(err)
	s.Empty(port.DeviceID)
	s.Empty(port.DeviceOwner)

	_, err = s.servers.AttachInterface(s.request("req-again"), serverID, app.InterfaceAttach{PortID: stopped.ID})
	s.Require().NoError(err)
	created, err := s.servers.AttachInterface(s.request("req-net"), serverID, app.InterfaceAttach{
		NetworkID: serverNetwork,
	})
	s.Require().NoError(err)
	s.Require().NoError(s.servers.Delete(s.request("req-delete"), serverID))

	port, err = s.network.GetPort(serverProject, stopped.ID)
	s.Require().NoError(err)
	s.Empty(port.DeviceID)
	_, err = s.network.GetPort(serverProject, created.PortID)
	s.ErrorIs(err, appnetwork.ErrPortNotFound)
}

// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/attach_interfaces.py#L144-L214
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/network/neutron.py#L914-L1064
func (s *ServersSuite) TestAttachInterfaceRejections() {
	serverID := s.bootOnDefaultNetwork("web")
	bare, err := s.network.Create(serverProject, "bare", false)
	s.Require().NoError(err)
	used := netip.MustParseAddr("10.20.0.2")
	bound := s.interfacePortIDs(serverID)[0]
	missing := "00000000-0000-4000-8000-0000000000ff"

	cases := []struct {
		name    string
		spec    app.InterfaceAttach
		kind    error
		message string
		action  bool
	}{
		{"both", app.InterfaceAttach{NetworkID: serverNetwork, PortID: bound}, app.ErrInvalidInterfaceRequest,
			"Must not input both network_id and port_id", false},
		{"address", app.InterfaceAttach{FixedIP: &used}, app.ErrInvalidInterfaceRequest,
			"Must input network_id when request IP address", false},
		{"port", app.InterfaceAttach{PortID: missing}, app.ErrPortNotFound,
			"Port id " + missing + " could not be found.", true},
		{"bound", app.InterfaceAttach{PortID: bound}, app.ErrPortInUse,
			"Port " + bound + " is still in use.", true},
		{"network", app.InterfaceAttach{NetworkID: missing}, app.ErrNetworkNotFound,
			"Network " + missing + " could not be found.", true},
		{"ambiguous", app.InterfaceAttach{}, app.ErrInvalidInterfaceRequest,
			"Multiple possible networks found, use a Network ID to be more specific.", true},
		{"subnet", app.InterfaceAttach{NetworkID: bare.ID}, app.ErrInvalidInterfaceRequest,
			"Network requires port_security_enabled and subnet associated in order to apply security groups.", true},
		{"used", app.InterfaceAttach{NetworkID: serverNetwork, FixedIP: &used}, app.ErrFixedIPInUse,
			"Fixed IP 10.20.0.2 is already in use.", true},
	}
	for _, tc := range cases {
		before := len(s.actionNames(serverID))

		_, err := s.servers.AttachInterface(s.request("req-"+tc.name), serverID, tc.spec)

		var interfaceErr *app.InterfaceError
		s.Require().ErrorAs(err, &interfaceErr, tc.name)
		s.ErrorIs(err, tc.kind, tc.name)
		s.Equal(tc.message, interfaceErr.Message, tc.name)
		recorded := len(s.actionNames(serverID)) - before
		if tc.action {
			s.Equal(1, recorded, tc.name)
		} else {
			s.Zero(recorded, tc.name)
		}
	}
	s.Equal([]string{bound}, s.interfacePortIDs(serverID))
}

// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/attach_interfaces.py#L113-L142
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/manager.py#L8728-L8744
func (s *ServersSuite) TestInterfaceStateChecksAndUnattachedPorts() {
	serverID := s.bootOnDefaultNetwork("web")
	free, err := s.network.CreatePort(serverProject, serverNetwork, "free", false, nil)
	s.Require().NoError(err)
	s.Require().NoError(s.compute.Lock(s.request("req-lock"), serverID))

	_, lockedAttach := s.servers.AttachInterface(s.request("req-locked"), serverID, app.InterfaceAttach{
		PortID: free.ID,
	})
	lockedDetach := s.servers.DetachInterface(s.request("req-locked"), serverID, free.ID)
	s.Require().NoError(s.compute.Unlock(s.request("req-unlock"), serverID))
	s.Require().NoError(s.compute.Suspend(s.request("req-suspend"), serverID))
	_, suspendedAttach := s.servers.AttachInterface(s.request("req-suspended"), serverID, app.InterfaceAttach{
		PortID: free.ID,
	})
	suspendedDetach := s.servers.DetachInterface(s.request("req-suspended"), serverID, free.ID)
	s.Require().NoError(s.compute.Resume(s.request("req-resume"), serverID))
	_, showErr := s.servers.GetInterface(serverProject, serverID, free.ID)
	unattachedDetach := s.servers.DetachInterface(s.request("req-unattached"), serverID, free.ID)

	s.ErrorIs(lockedAttach, app.ErrServerLocked)
	s.EqualError(lockedDetach, "Instance "+serverID+" is locked")
	var stateErr *app.InvalidStateError
	s.Require().ErrorAs(suspendedAttach, &stateErr)
	s.Equal("Cannot 'attach_interface' instance "+serverID+" while it is in vm_state suspended", stateErr.Message)
	s.EqualError(suspendedDetach,
		"Cannot 'detach_interface' instance "+serverID+" while it is in vm_state suspended")
	s.ErrorIs(showErr, app.ErrInterfaceNotAttached)
	s.EqualError(showErr, "Instance "+serverID+" does not have a port with id "+free.ID)
	s.NoError(unattachedDetach)
	port, err := s.network.GetPort(serverProject, free.ID)
	s.Require().NoError(err)
	s.Empty(port.DeviceID)
	s.Equal([]string{
		domain.ActionDetachInterface, domain.ActionResume, domain.ActionSuspend, domain.ActionUnlock,
		domain.ActionLock, domain.ActionCreate,
	}, s.actionNames(serverID))
}
