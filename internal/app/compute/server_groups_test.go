package compute_test

import (
	"fmt"

	app "github.com/JSYoo5B/sandstack/internal/app/compute"
	domain "github.com/JSYoo5B/sandstack/internal/domain/compute"
)

const noValidHost = "No valid host was found. There are not enough hosts available."

func (s *ServersSuite) serverGroups() *app.ServerGroups {
	return app.NewServerGroups(s.compute, sequence("40000000-0000-4000-8000-"))
}

func (s *ServersSuite) groupMember(groupID, name string) app.CreatedServer {
	created, err := s.servers.Create(s.request("req-"+name), app.ServerCreate{
		Name: name, ImageID: serverImage, FlavorID: app.FixtureFlavorID, GroupID: groupID,
	})
	s.Require().NoError(err)
	return created
}

// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/server_groups.py#L133-L283
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/objects/instance_group.py#L42-L50
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/conf/quota.py#L157-L183
func (s *ServersSuite) TestServerGroupsAreProjectScopedWithQuota() {
	groups := s.serverGroups()
	web, err := groups.Create(s.request("req-web"), "web", domain.PolicyAntiAffinity)
	s.Require().NoError(err)
	db, err := groups.Create(s.request("req-db"), "db", domain.PolicyAffinity)
	s.Require().NoError(err)
	_, err = groups.Create(app.ActionRequest{ProjectID: "other", UserID: "other"}, "other", domain.PolicyAffinity)
	s.Require().NoError(err)

	listed := groups.List(serverProject)
	shown, getErr := groups.Get(serverProject, web.ID)
	_, otherErr := groups.Get("other", web.ID)

	s.Equal([]domain.ServerGroup{
		{ID: web.ID, Name: "web", ProjectID: serverProject, UserID: serverUser,
			Policy: domain.PolicyAntiAffinity, Members: []string{}},
		{ID: db.ID, Name: "db", ProjectID: serverProject, UserID: serverUser,
			Policy: domain.PolicyAffinity, Members: []string{}},
	}, listed)
	s.Require().NoError(getErr)
	s.Equal(web, shown)
	s.ErrorIs(otherErr, app.ErrServerGroupNotFound)
	s.ErrorIs(groups.Delete("other", web.ID), app.ErrServerGroupNotFound)

	s.Require().NoError(groups.Delete(serverProject, web.ID))

	_, err = groups.Get(serverProject, web.ID)
	s.ErrorIs(err, app.ErrServerGroupNotFound)
	s.ErrorIs(groups.Delete(serverProject, web.ID), app.ErrServerGroupNotFound)
	for i := range 9 {
		_, err := groups.Create(s.request("req-fill"), fmt.Sprintf("group-%d", i), domain.PolicyAffinity)
		s.Require().NoError(err)
	}
	_, err = groups.Create(s.request("req-over"), "over", domain.PolicyAffinity)
	s.ErrorIs(err, app.ErrServerGroupQuota)
	s.Len(groups.List(serverProject), 10)

	s.compute.Reset()

	s.Empty(groups.List(serverProject))
	s.Len(groups.List("other"), 0)
}

// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L1577-L1626
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L1667-L1678
func (s *ServersSuite) TestBootJoinsGroupAndDeleteLeavesIt() {
	s.addSubnet(serverNetwork, "10.20.0.0/24")
	groups := s.serverGroups()
	group, err := groups.Create(s.request("req-group"), "web", domain.PolicyAffinity)
	s.Require().NoError(err)

	first := s.groupMember(group.ID, "first")
	second := s.groupMember(group.ID, "second")

	s.Equal(domain.StatusActive, first.Server.Status())
	s.Equal(domain.StatusActive, second.Server.Status())
	shown, err := groups.Get(serverProject, group.ID)
	s.Require().NoError(err)
	s.Equal([]string{first.Server.ID, second.Server.ID}, shown.Members)

	s.Require().NoError(s.servers.Delete(s.request("req-delete"), first.Server.ID))

	shown, err = groups.Get(serverProject, group.ID)
	s.Require().NoError(err)
	s.Equal([]string{second.Server.ID}, shown.Members)
}

// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/servers.py#L835-L837
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/servers.py#L890-L902
func (s *ServersSuite) TestBootRejectsUnknownGroupAndMemberQuota() {
	s.addSubnet(serverNetwork, "10.20.0.0/24")
	groups := s.serverGroups()
	other, err := groups.Create(app.ActionRequest{ProjectID: "other", UserID: "other"}, "other", domain.PolicyAffinity)
	s.Require().NoError(err)
	group, err := groups.Create(s.request("req-group"), "web", domain.PolicyAffinity)
	s.Require().NoError(err)
	for i := range 10 {
		s.groupMember(group.ID, fmt.Sprintf("member-%d", i))
	}
	before := len(s.servers.List(serverProject, app.ListFilter{}))

	_, unknownErr := s.servers.Create(s.request("req-unknown"), app.ServerCreate{
		Name: "web", ImageID: serverImage, FlavorID: app.FixtureFlavorID, GroupID: other.ID,
	})
	_, quotaErr := s.servers.Create(s.request("req-quota"), app.ServerCreate{
		Name: "web", ImageID: serverImage, FlavorID: app.FixtureFlavorID, GroupID: group.ID,
	})

	s.ErrorIs(unknownErr, app.ErrServerGroupNotFound)
	s.EqualError(unknownErr, "Instance group "+other.ID+" could not be found.")
	s.ErrorIs(quotaErr, app.ErrServerGroupMemberQuota)
	s.EqualError(quotaErr, "Quota exceeded, too many servers in group")
	s.Len(s.servers.List(serverProject, app.ListFilter{}), before)
	shown, err := groups.Get(serverProject, group.ID)
	s.Require().NoError(err)
	s.Len(shown.Members, 10)
}

// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/scheduler/filters/affinity_filter.py#L82-L135
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/scheduler/manager.py#L542-L572
func (s *ServersSuite) TestAntiAffinityMemberFailsSchedulingOnTheOnlyHost() {
	s.addSubnet(serverNetwork, "10.20.0.0/24")
	groups := s.serverGroups()
	group, err := groups.Create(s.request("req-group"), "web", domain.PolicyAntiAffinity)
	s.Require().NoError(err)
	first := s.groupMember(group.ID, "first")

	second := s.groupMember(group.ID, "second")

	s.Equal(domain.StatusActive, first.Server.Status())
	s.Equal(domain.StatusError, second.Server.Status())
	s.Equal(&domain.Fault{Code: 500, Message: noValidHost, CreatedAt: s.start}, second.Server.Fault)
	s.Empty(second.Server.Ports)
	s.Empty(s.network.DevicePorts(serverProject, second.Server.ID))
	shown, err := groups.Get(serverProject, group.ID)
	s.Require().NoError(err)
	s.Equal([]string{first.Server.ID, second.Server.ID}, shown.Members)

	s.Require().NoError(s.servers.Delete(s.request("req-delete"), first.Server.ID))
	third := s.groupMember(group.ID, "third")

	s.Equal(domain.StatusActive, third.Server.Status())
	s.Len(third.Server.Ports, 1)
}

// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/scheduler/filters/affinity_filter.py#L82-L135
func (s *ServersSuite) TestHeldAntiAffinityBuildsScheduleOnCompletion() {
	s.addSubnet(serverNetwork, "10.20.0.0/24")
	groups := s.serverGroups()
	group, err := groups.Create(s.request("req-group"), "web", domain.PolicyAntiAffinity)
	s.Require().NoError(err)
	s.compute.SetHoldBuilds(true)
	first := s.groupMember(group.ID, "first")
	second := s.groupMember(group.ID, "second")
	s.Require().Equal(domain.StatusBuild, second.Server.Status())

	s.Require().NoError(s.servers.FinishBuild(first.Server.ID, false))
	s.Require().NoError(s.servers.FinishBuild(second.Server.ID, false))

	active, err := s.servers.Get(serverProject, first.Server.ID)
	s.Require().NoError(err)
	s.Equal(domain.StatusActive, active.Status())
	errored, err := s.servers.Get(serverProject, second.Server.ID)
	s.Require().NoError(err)
	s.Equal(domain.StatusError, errored.Status())
	s.Equal(&domain.Fault{Code: 500, Message: noValidHost, CreatedAt: s.start}, errored.Fault)
	s.ErrorIs(s.servers.FinishBuild(second.Server.ID, false), app.ErrBuildNotPending)
}
