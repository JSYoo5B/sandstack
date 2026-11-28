package compute_test

import (
	app "github.com/JSYoo5B/sandstack/internal/app/compute"
	domain "github.com/JSYoo5B/sandstack/internal/domain/compute"
	imagedomain "github.com/JSYoo5B/sandstack/internal/domain/image"
)

func (s *ServersSuite) bootWithFlavor(name, imageID, flavorID string) string {
	created, err := s.servers.Create(s.request("req-"+name), app.ServerCreate{
		Name: name, ImageID: imageID, FlavorID: flavorID,
	})
	s.Require().NoError(err)
	return created.Server.ID
}

func (s *ServersSuite) customImage(minRAM int, properties map[string]string) string {
	image, err := s.images.CreateSnapshot(serverProject, imagedomain.Image{
		Name: "custom", MinRAMMiB: minRAM, Properties: properties,
	})
	s.Require().NoError(err)
	s.Require().NoError(s.images.CompleteSnapshot(serverProject, image.ID, serverImage, "bare", "qcow2", nil))
	return image.ID
}

func (s *ServersSuite) actionNames(serverID string) []string {
	actions, err := s.compute.ListActions(serverProject, serverID)
	s.Require().NoError(err)
	names := make([]string, len(actions))
	for i, action := range actions {
		names[i] = action.Name
	}
	return names
}

// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L4304-L4513
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/instance_actions.py#L38-L40
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/manager.py#L6482-L6488
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/manager.py#L6645-L6648
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/manager.py#L5112-L5130
func (s *ServersSuite) TestResizeAwaitsConfirmationWithNewFlavor() {
	s.addSubnet(serverNetwork, "10.20.0.0/24")
	serverID := s.bootFromImage("web", serverImage)

	resizeErr := s.servers.Resize(s.request("req-resize"), serverID, app.FixtureSmallFlavorID, nil)

	s.Require().NoError(resizeErr)
	resized, err := s.servers.Get(serverProject, serverID)
	s.Require().NoError(err)
	s.Equal(domain.StatusResized, resized.Status())
	s.Equal(app.FixtureSmallFlavorID, resized.FlavorID)
	s.Empty(resized.TaskState)

	confirmErr := s.compute.ConfirmResize(s.request("req-confirm"), serverID)

	s.Require().NoError(confirmErr)
	confirmed, err := s.servers.Get(serverProject, serverID)
	s.Require().NoError(err)
	s.Equal(domain.StatusActive, confirmed.Status())
	s.Equal(app.FixtureSmallFlavorID, confirmed.FlavorID)
	s.Nil(confirmed.Resize)
	s.Equal([]string{domain.ActionConfirmResize, domain.ActionResize, domain.ActionCreate}, s.actionNames(serverID))
}

// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L4019-L4107
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/manager.py#L6526-L6581
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/manager.py#L5755-L5790
func (s *ServersSuite) TestRevertResizeRestoresFlavorAndStoppedState() {
	s.addSubnet(serverNetwork, "10.20.0.0/24")
	serverID := s.bootFromImage("web", serverImage)
	s.Require().NoError(s.compute.SetPower(s.request("req-stop"), serverID, false))
	s.Require().NoError(s.servers.Resize(s.request("req-resize"), serverID, app.FixtureSmallFlavorID, nil))
	resized, err := s.servers.Get(serverProject, serverID)
	s.Require().NoError(err)
	s.Require().Equal(domain.StatusResized, resized.Status())
	s.Require().Equal(&domain.ResizeOrigin{FlavorID: app.FixtureFlavorID, State: domain.StatusShutoff}, resized.Resize)

	revertErr := s.compute.RevertResize(s.request("req-revert"), serverID)

	s.Require().NoError(revertErr)
	reverted, err := s.servers.Get(serverProject, serverID)
	s.Require().NoError(err)
	s.Equal(domain.StatusShutoff, reverted.Status())
	s.Equal(app.FixtureFlavorID, reverted.FlavorID)
	s.Nil(reverted.Resize)
	action, err := s.compute.GetAction(serverProject, serverID, "req-revert")
	s.Require().NoError(err)
	s.Equal(domain.ActionRevertResize, action.Name)
}

// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/servers.py#L1149-L1216
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L4340-L4434
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L781-L898
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/utils.py#L536-L561
func (s *ServersSuite) TestResizeRejectsInvalidFlavorsBeforeChangingState() {
	s.addSubnet(serverNetwork, "10.20.0.0/24")
	small := s.bootWithFlavor("small", serverImage, app.FixtureSmallFlavorID)
	hungry := s.bootWithFlavor("hungry", s.customImage(1024, nil), app.FixtureSmallFlavorID)
	manual := s.bootFromImage("manual", s.customImage(0, map[string]string{"auto_disk_config": "Disabled"}))
	auto := true

	for _, tt := range []struct {
		name     string
		serverID string
		flavorID string
		auto     *bool
		message  string
	}{
		{
			name: "unknown flavor", serverID: small, flavorID: "999",
			message: "Flavor 999 could not be found.",
		},
		{
			name: "same flavor", serverID: small, flavorID: app.FixtureSmallFlavorID,
			message: "When resizing, instances must change flavor!",
		},
		{
			name: "root disk below image minimum", serverID: small, flavorID: app.FixtureFlavorID,
			message: "Flavor's disk is smaller than the minimum size specified in image metadata. " +
				"Flavor disk is 1073741824 bytes, minimum size is 21474836480 bytes.",
		},
		{
			name: "memory below image minimum", serverID: hungry, flavorID: app.FixtureFlavorID,
			message: "Flavor's memory is too small for requested image.",
		},
		{
			name: "automatic disk config disabled", serverID: manual, flavorID: app.FixtureSmallFlavorID,
			auto: &auto, message: "Requested image " + s.mustImageRef(manual) + " has automatic disk resize disabled.",
		},
	} {
		s.Run(tt.name, func() {
			err := s.servers.Resize(s.request("req-"+tt.name), tt.serverID, tt.flavorID, tt.auto)

			s.ErrorIs(err, app.ErrInvalidResize)
			s.EqualError(err, tt.message)
			detail, getErr := s.servers.Get(serverProject, tt.serverID)
			s.Require().NoError(getErr)
			s.NotEqual(domain.StatusResized, detail.Status())
			s.Nil(detail.Resize)
		})
	}
}

func (s *ServersSuite) mustImageRef(serverID string) string {
	detail, err := s.servers.Get(serverProject, serverID)
	s.Require().NoError(err)
	return detail.ImageID
}

// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L4409-L4434
func (s *ServersSuite) TestResizeVolumeBackedServerSkipsRootDiskCheck() {
	s.addSubnet(serverNetwork, "10.20.0.0/24")
	spec := volumeBoot("web", app.BlockDevice{
		SourceType: app.BlockDeviceSourceVolume, UUID: s.bootableVolume(), BootIndex: bootIndex(0),
	})
	spec.FlavorID = app.FixtureSmallFlavorID
	created, err := s.servers.Create(s.request("req-create"), spec)
	s.Require().NoError(err)

	resizeErr := s.servers.Resize(s.request("req-resize"), created.Server.ID, app.FixtureFlavorID, nil)

	s.Require().NoError(resizeErr)
	detail, err := s.servers.Get(serverProject, created.Server.ID)
	s.Require().NoError(err)
	s.Equal(domain.StatusResized, detail.Status())
	s.Equal(app.FixtureFlavorID, detail.FlavorID)
}

// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L142-L180
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L247-L253
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L4130-L4131
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/common.py#L330-L346
func (s *ServersSuite) TestResizeActionsCheckLockThenState() {
	s.addSubnet(serverNetwork, "10.20.0.0/24")
	serverID := s.bootFromImage("web", serverImage)

	confirmErr := s.compute.ConfirmResize(s.request("req-confirm"), serverID)
	revertErr := s.compute.RevertResize(s.request("req-revert"), serverID)
	s.Require().NoError(s.compute.Lock(s.request("req-lock"), serverID))
	lockedErr := s.servers.Resize(s.request("req-locked"), serverID, app.FixtureSmallFlavorID, nil)
	s.Require().NoError(s.compute.Unlock(s.request("req-unlock"), serverID))
	s.Require().NoError(s.servers.Resize(s.request("req-resize"), serverID, app.FixtureSmallFlavorID, nil))
	againErr := s.servers.Resize(s.request("req-again"), serverID, app.FixtureFlavorID, nil)

	var stateErr *app.InvalidStateError
	s.ErrorAs(confirmErr, &stateErr)
	s.EqualError(confirmErr, "Cannot 'confirmResize' instance "+serverID+" while it is in vm_state active")
	s.EqualError(revertErr, "Cannot 'revertResize' instance "+serverID+" while it is in vm_state active")
	s.ErrorIs(lockedErr, app.ErrServerLocked)
	s.EqualError(lockedErr, "Instance "+serverID+" is locked")
	s.EqualError(againErr, "Cannot 'resize' instance "+serverID+" while it is in vm_state resized")
}

// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L2882-L2885
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L3454-L3460
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L5225-L5230
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/vm_states.py#L69-L73
func (s *ServersSuite) TestResizedServerAcceptsOnlyUpstreamActions() {
	s.addSubnet(serverNetwork, "10.20.0.0/24")
	serverID := s.bootFromImage("web", serverImage)
	s.Require().NoError(s.servers.Resize(s.request("req-resize"), serverID, app.FixtureSmallFlavorID, nil))
	attachments := app.NewAttachments(s.compute, s.volumes)

	stopErr := s.compute.SetPower(s.request("req-stop"), serverID, false)
	rebootErr := s.compute.Reboot(s.request("req-reboot"), serverID, true)
	_, imageErr := s.servers.CreateImage(s.request("req-image"), serverID, "snap", nil)
	_, attachErr := attachments.Attach(s.request("req-attach"), serverID, serverVolume, "")

	s.ErrorIs(stopErr, app.ErrServerPowerState)
	s.ErrorIs(rebootErr, app.ErrServerPowerState)
	s.EqualError(imageErr, "Cannot 'createImage' instance "+serverID+" while it is in vm_state resized")
	s.NoError(attachErr)
	detail, err := s.servers.Get(serverProject, serverID)
	s.Require().NoError(err)
	s.Equal(domain.StatusResized, detail.Status())
	s.Equal([]string{serverVolume}, detail.AttachedVolumeIDs)
}
