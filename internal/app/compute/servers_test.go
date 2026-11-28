package compute_test

import (
	"fmt"
	"net/netip"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	app "github.com/JSYoo5B/sandstack/internal/app/compute"
	appimage "github.com/JSYoo5B/sandstack/internal/app/image"
	appnetwork "github.com/JSYoo5B/sandstack/internal/app/network"
	appvolume "github.com/JSYoo5B/sandstack/internal/app/volume"
	domain "github.com/JSYoo5B/sandstack/internal/domain/compute"
	imagedomain "github.com/JSYoo5B/sandstack/internal/domain/image"
	networkdomain "github.com/JSYoo5B/sandstack/internal/domain/network"
	volumedomain "github.com/JSYoo5B/sandstack/internal/domain/volume"
)

const (
	serverProject = "project"
	serverUser    = "user"
	serverImage   = "962a0750-d2e0-4400-88e0-a9ad35758eac"
	serverNetwork = "43d92fea-ce36-4d02-aba4-29806839f902"
	serverVolume  = "7b37635e-be9b-4bfb-bc2e-d163b75921e6"
)

type ServersSuite struct {
	suite.Suite
	start   time.Time
	compute *app.Service
	servers *app.Servers
	network *appnetwork.Service
	images  *appimage.Service
	volumes *appvolume.Service
	keys    *app.KeyPairs
}

func TestServersSuite(t *testing.T) {
	suite.Run(t, new(ServersSuite))
}

func (s *ServersSuite) SetupTest() {
	s.start = time.Date(2026, time.November, 28, 1, 2, 3, 0, time.UTC)
	now := func() time.Time { return s.start }
	s.network = appnetwork.New(networkdomain.Network{
		ID: serverNetwork, Name: "fixture-network", ProjectID: serverProject,
	}, sequence("00000000-0000-4000-8000-"))
	s.images = appimage.New(imagedomain.Image{
		ID: serverImage, Name: "fixture-image", ProjectID: serverProject,
	}, now, "image", sequence("10000000-0000-4000-8000-"))
	s.volumes = appvolume.New(volumedomain.Volume{
		ID: serverVolume, ProjectID: serverProject, Size: 1,
	}, now, sequence("20000000-0000-4000-8000-"))
	s.compute = app.New(domain.Server{
		ID: "fixture", ProjectID: serverProject, UserID: serverUser, CreatedAt: s.start.Add(-time.Hour),
	}, now, s.volumes)
	s.keys = app.NewKeyPairs(now, nil)
	passwords := 0
	s.servers = app.NewServers(s.compute, s.images, s.network, s.volumes, s.keys,
		sequence("30000000-0000-4000-8000-"), func() string {
			passwords++
			return fmt.Sprintf("password-%d", passwords)
		})
}

func sequence(prefix string) func() string {
	next := 0
	return func() string {
		next++
		return fmt.Sprintf("%s%012d", prefix, next)
	}
}

func (s *ServersSuite) request(id string) app.ActionRequest {
	return app.ActionRequest{ProjectID: serverProject, UserID: serverUser, RequestID: id}
}

func (s *ServersSuite) addSubnet(networkID, cidr string) {
	_, err := s.network.CreateSubnet(serverProject, networkID, appnetwork.SubnetCreate{
		CIDR: netip.MustParsePrefix(cidr),
	})
	s.Require().NoError(err)
}

// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/network/neutron.py#L976-L998
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/views/servers.py#L609-L619
func (s *ServersSuite) TestCreateOnDefaultNetworkCreatesBoundPort() {
	s.addSubnet(serverNetwork, "10.20.0.0/24")
	_, err := s.keys.Import(serverUser, "deploy", importedKey)
	s.Require().NoError(err)

	created, err := s.servers.Create(s.request("req-create"), app.ServerCreate{
		Name: "web", ImageID: serverImage, FlavorID: app.FixtureFlavorID, KeyName: "deploy",
		Metadata: map[string]string{"role": "web"},
	})

	s.Require().NoError(err)
	serverID := "30000000-0000-4000-8000-000000000001"
	s.Equal("password-1", created.AdminPass)
	s.Equal(serverID, created.Server.ID)
	detail, err := s.servers.Get(serverProject, serverID)
	s.Require().NoError(err)
	s.Equal(domain.StatusActive, detail.Status())
	s.Equal(serverUser, detail.UserID)
	s.Equal("deploy", detail.KeyName)
	s.Equal(map[string]string{"role": "web"}, detail.Metadata)
	s.Equal(s.start, detail.CreatedAt)
	s.Require().Len(detail.Ports, 1)
	s.False(detail.Ports[0].Preserve)
	port, err := s.network.GetPort(serverProject, detail.Ports[0].ID)
	s.Require().NoError(err)
	s.Equal(serverID, port.DeviceID)
	s.Equal(networkdomain.DeviceOwnerCompute, port.DeviceOwner)
	s.Equal([]string{"default"}, detail.SecurityGroups)
	s.Equal([]app.ServerNetwork{{
		Label:     "fixture-network",
		Addresses: []app.ServerAddress{{Address: netip.MustParseAddr("10.20.0.2"), MAC: port.MACAddress}},
	}}, detail.Networks)
	actions, err := s.compute.ListActions(serverProject, serverID)
	s.Require().NoError(err)
	s.Equal([]domain.Action{{
		Name: domain.ActionCreate, ServerID: serverID, RequestID: "req-create",
		ProjectID: serverProject, UserID: serverUser, StartTime: s.start,
	}}, actions)
	listed := s.servers.List(serverProject, app.ListFilter{})
	s.Require().Len(listed, 2)
	s.Equal(serverID, listed[0].ID)
	s.Equal("fixture", listed[1].ID)
}

// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/network/neutron.py#L2523-L2623
func (s *ServersSuite) TestCreateRejectsRequestsBeforeRecordingServer() {
	other, err := s.network.Create(serverProject, "other", false)
	s.Require().NoError(err)
	_, err = s.network.CreateSecurityGroup(serverProject, "twin", "", true)
	s.Require().NoError(err)
	_, err = s.network.CreateSecurityGroup(serverProject, "twin", "", true)
	s.Require().NoError(err)
	base := app.ServerCreate{Name: "web", ImageID: serverImage, FlavorID: app.FixtureFlavorID}
	with := func(change func(*app.ServerCreate)) app.ServerCreate {
		spec := base
		change(&spec)
		return spec
	}
	cases := []struct {
		name string
		spec app.ServerCreate
		want error
	}{
		{"unknown flavor", with(func(c *app.ServerCreate) { c.FlavorID = "42" }), app.ErrFlavorNotFound},
		{"unknown image", with(func(c *app.ServerCreate) { c.ImageID = other.ID }), app.ErrImageNotFound},
		{"unknown group", with(func(c *app.ServerCreate) { c.SecurityGroups = []string{"web"} }),
			app.ErrSecurityGroupNotFound},
		{"ambiguous group", with(func(c *app.ServerCreate) { c.SecurityGroups = []string{"twin"} }),
			app.ErrSecurityGroupAmbiguous},
		{"ambiguous default network", base, app.ErrNetworkAmbiguous},
		{"network without subnet", with(func(c *app.ServerCreate) {
			c.Networks = []app.NetworkRequest{{NetworkID: other.ID}}
		}), app.ErrNetworkRequiresSubnet},
		{"unknown network", with(func(c *app.ServerCreate) {
			c.Networks = []app.NetworkRequest{{NetworkID: "00000000-0000-4000-8000-999999999999"}}
		}), app.ErrNetworkNotFound},
		{"unknown port", with(func(c *app.ServerCreate) {
			c.Networks = []app.NetworkRequest{{PortID: "00000000-0000-4000-8000-999999999999"}}
		}), app.ErrPortNotFound},
		{"unbound port without address", with(func(c *app.ServerCreate) {
			port, err := s.network.CreatePort(serverProject, other.ID, "", false, nil)
			s.Require().NoError(err)
			c.Networks = []app.NetworkRequest{{PortID: port.ID}}
		}), app.ErrPortRequiresFixedIP},
	}
	for _, test := range cases {
		s.Run(test.name, func() {
			_, err := s.servers.Create(s.request("req"), test.spec)

			s.ErrorIs(err, test.want)
		})
	}
	s.Len(s.servers.List(serverProject, app.ListFilter{}), 1)
	s.Len(s.network.ListPorts(serverProject, appnetwork.PortListFilter{
		DeviceOwners: []string{networkdomain.DeviceOwnerCompute},
	}), 0)
}

// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L1140-L1162
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L745-L746
func (s *ServersSuite) TestCreateChecksKeyMetadataAndImageAfterNetworks() {
	s.addSubnet(serverNetwork, "10.20.0.0/24")
	base := app.ServerCreate{Name: "web", ImageID: serverImage, FlavorID: app.FixtureFlavorID}
	metadata := map[string]string{}
	for i := range 129 {
		metadata[fmt.Sprintf("key-%d", i)] = "value"
	}

	_, keyErr := s.servers.Create(s.request("req"), app.ServerCreate{
		Name: base.Name, ImageID: base.ImageID, FlavorID: base.FlavorID, KeyName: "missing",
	})
	_, quotaErr := s.servers.Create(s.request("req"), app.ServerCreate{
		Name: base.Name, ImageID: base.ImageID, FlavorID: base.FlavorID, Metadata: metadata,
	})
	s.Require().NoError(s.images.SetDeactivated(serverImage, true))
	_, imageErr := s.servers.Create(s.request("req"), base)

	s.ErrorIs(keyErr, app.ErrKeyPairNotFound)
	s.ErrorIs(quotaErr, app.ErrServerMetadataQuota)
	s.ErrorIs(imageErr, app.ErrImageNotActive)
	s.EqualError(imageErr, "Image "+serverImage+" is not active.")
	s.Len(s.servers.List(serverProject, app.ListFilter{}), 1)
}

// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/network/neutron.py#L1022-L1044
func (s *ServersSuite) TestDefaultNetworkWithoutSubnetFailsTheBuild() {
	created, err := s.servers.Create(s.request("req"), app.ServerCreate{
		Name: "web", ImageID: serverImage, FlavorID: app.FixtureFlavorID,
	})

	s.Require().NoError(err)
	detail, err := s.servers.Get(serverProject, created.Server.ID)
	s.Require().NoError(err)
	s.Equal(domain.StatusError, detail.Status())
	s.Empty(detail.Ports)
	s.Equal(&domain.Fault{
		Code: 500,
		Message: "Build of instance " + created.Server.ID + " aborted: Network requires port_security_enabled " +
			"and subnet associated in order to apply security groups.",
		CreatedAt: s.start,
	}, detail.Fault)
}

// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/network/neutron.py#L1841-L1876
func (s *ServersSuite) TestDeleteReleasesPortsAndVolumes() {
	s.addSubnet(serverNetwork, "10.20.0.0/24")
	existing, err := s.network.CreatePort(serverProject, serverNetwork, "kept", false, nil)
	s.Require().NoError(err)
	created, err := s.servers.Create(s.request("req-create"), app.ServerCreate{
		Name: "web", ImageID: serverImage, FlavorID: app.FixtureFlavorID,
		Networks: []app.NetworkRequest{{PortID: existing.ID}, {NetworkID: serverNetwork}},
	})
	s.Require().NoError(err)
	serverID := created.Server.ID
	s.Require().Len(created.Server.Ports, 2)
	createdPortID := created.Server.Ports[1].ID
	attachments := app.NewAttachments(s.compute, s.volumes)
	_, err = attachments.Attach(s.request("req-attach"), serverID, serverVolume, "")
	s.Require().NoError(err)
	s.Require().NoError(s.compute.Lock(s.request("req-lock"), serverID))

	lockedErr := s.servers.Delete(s.request("req-delete"), serverID)
	s.Require().NoError(s.compute.Unlock(s.request("req-unlock"), serverID))
	err = s.servers.Delete(s.request("req-delete"), serverID)

	s.ErrorIs(lockedErr, app.ErrServerLocked)
	s.Require().NoError(err)
	_, err = s.servers.Get(serverProject, serverID)
	s.ErrorIs(err, app.ErrServerNotFound)
	s.ErrorIs(s.servers.Delete(s.request("req-again"), serverID), app.ErrServerNotFound)
	kept, err := s.network.GetPort(serverProject, existing.ID)
	s.Require().NoError(err)
	s.Empty(kept.DeviceID)
	s.Empty(kept.DeviceOwner)
	_, err = s.network.GetPort(serverProject, createdPortID)
	s.ErrorIs(err, appnetwork.ErrPortNotFound)
	volume, err := s.volumes.Get(serverProject, serverVolume)
	s.Require().NoError(err)
	s.Equal(volumedomain.StateAvailable, volume.State)
}

// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/network/neutron.py#L2557-L2581
func (s *ServersSuite) TestBoundPortIsInUse() {
	s.addSubnet(serverNetwork, "10.20.0.0/24")
	existing, err := s.network.CreatePort(serverProject, serverNetwork, "", false, nil)
	s.Require().NoError(err)
	spec := app.ServerCreate{
		Name: "web", ImageID: serverImage, FlavorID: app.FixtureFlavorID,
		Networks: []app.NetworkRequest{{PortID: existing.ID}},
	}
	_, err = s.servers.Create(s.request("req-first"), spec)
	s.Require().NoError(err)

	_, err = s.servers.Create(s.request("req-second"), spec)

	s.ErrorIs(err, app.ErrPortInUse)
	s.Len(s.servers.List(serverProject, app.ListFilter{}), 2)
}

func (s *ServersSuite) TestHeldBuildHidesAddressesUntilFinished() {
	s.addSubnet(serverNetwork, "10.20.0.0/24")
	s.compute.SetHoldBuilds(true)
	spec := app.ServerCreate{Name: "web", ImageID: serverImage, FlavorID: app.FixtureFlavorID}
	completed, err := s.servers.Create(s.request("req-complete"), spec)
	s.Require().NoError(err)
	failed, err := s.servers.Create(s.request("req-fail"), spec)
	s.Require().NoError(err)

	building, err := s.servers.Get(serverProject, completed.Server.ID)

	s.Require().NoError(err)
	s.Equal(domain.StatusBuild, building.Status())
	s.Empty(building.Networks)
	s.Equal([]string{"default"}, building.SecurityGroups)

	s.start = s.start.Add(time.Minute)
	s.Require().NoError(s.compute.FinishBuild(completed.Server.ID, false))
	s.Require().NoError(s.compute.FinishBuild(failed.Server.ID, true))

	active, err := s.servers.Get(serverProject, completed.Server.ID)
	s.Require().NoError(err)
	s.Equal(domain.StatusActive, active.Status())
	s.Len(active.Networks, 1)
	s.Equal(s.start, active.UpdatedAt)
	errored, err := s.servers.Get(serverProject, failed.Server.ID)
	s.Require().NoError(err)
	s.Equal(domain.StatusError, errored.Status())
	s.Equal(&domain.Fault{
		Code: 500, Message: "No valid host was found. There are not enough hosts available.", CreatedAt: s.start,
	}, errored.Fault)
	s.NoError(s.compute.FinishBuild(completed.Server.ID, false))
	s.ErrorIs(s.compute.FinishBuild(completed.Server.ID, true), app.ErrBuildNotPending)
	s.ErrorIs(s.compute.FinishBuild("missing", true), app.ErrServerNotFound)

	s.compute.Reset()

	s.Len(s.servers.List(serverProject, app.ListFilter{}), 1)
	reset, err := s.servers.Create(s.request("req-reset"), spec)
	s.Require().NoError(err)
	assert.Equal(s.T(), domain.StatusActive, reset.Server.Status())
}

func TestServersRejectAnotherProject(t *testing.T) {
	compute := app.New(domain.Server{ID: "fixture", ProjectID: serverProject}, time.Now, nil)
	servers := app.NewServers(compute, nil, nil, nil, nil, nil, nil)

	_, err := servers.Get("other", "fixture")
	deleteErr := servers.Delete(app.ActionRequest{ProjectID: "other"}, "fixture")

	assert.ErrorIs(t, err, app.ErrServerNotFound)
	assert.ErrorIs(t, deleteErr, app.ErrServerNotFound)
	_, err = servers.Get(serverProject, "fixture")
	require.NoError(t, err)
}
