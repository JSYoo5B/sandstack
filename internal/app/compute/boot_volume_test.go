package compute_test

import (
	"time"

	app "github.com/JSYoo5B/sandstack/internal/app/compute"
	appimage "github.com/JSYoo5B/sandstack/internal/app/image"
	appnetwork "github.com/JSYoo5B/sandstack/internal/app/network"
	appvolume "github.com/JSYoo5B/sandstack/internal/app/volume"
	domain "github.com/JSYoo5B/sandstack/internal/domain/compute"
	networkdomain "github.com/JSYoo5B/sandstack/internal/domain/network"
	volumedomain "github.com/JSYoo5B/sandstack/internal/domain/volume"
)

const missingVolume = "20000000-0000-4000-8000-999999999999"

func bootIndex(index int) *int {
	return &index
}

func (s *ServersSuite) bootableVolume() string {
	volume, err := s.volumes.CreateFromImage(serverProject, serverImage, "root", "", 1, nil)
	s.Require().NoError(err)
	s.Require().NoError(s.volumes.CompleteCreation(volume.ID))
	return volume.ID
}

func volumeBoot(name string, device app.BlockDevice) app.ServerCreate {
	return app.ServerCreate{Name: name, FlavorID: app.FixtureFlavorID, BlockDevices: []app.BlockDevice{device}}
}

// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/block_device.py#L615-L695
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/volume_attachments.py#L426-L428
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L2695-L2724
func (s *ServersSuite) TestBootFromExistingVolumeAttachesRootDevice() {
	s.addSubnet(serverNetwork, "10.20.0.0/24")
	rootID := s.bootableVolume()
	attachments := app.NewAttachments(s.compute, s.volumes)

	created, err := s.servers.Create(s.request("req-create"), volumeBoot("web", app.BlockDevice{
		SourceType: app.BlockDeviceSourceVolume, UUID: rootID, BootIndex: bootIndex(0),
	}))

	s.Require().NoError(err)
	serverID := created.Server.ID
	detail, err := s.servers.Get(serverProject, serverID)
	s.Require().NoError(err)
	s.Equal(domain.StatusActive, detail.Status())
	s.Empty(detail.ImageID)
	s.Equal([]string{rootID}, detail.AttachedVolumeIDs)
	root, err := s.volumes.Get(serverProject, rootID)
	s.Require().NoError(err)
	s.Equal(volumedomain.StateInUse, root.State)
	s.Equal(serverID, root.Attachment.ServerID)
	s.Equal("/dev/vda", root.Attachment.Device)
	extra, err := attachments.Attach(s.request("req-attach"), serverID, serverVolume, "")
	s.Require().NoError(err)
	s.Equal("/dev/vdb", extra.Device)

	detachErr := attachments.Detach(s.request("req-detach"), serverID, rootID)
	s.Require().NoError(s.servers.Delete(s.request("req-delete"), serverID))

	s.ErrorIs(detachErr, app.ErrRootVolumeDetach)
	s.EqualError(detachErr, "Cannot detach a root device volume")
	for _, volumeID := range []string{rootID, serverVolume} {
		volume, err := s.volumes.Get(serverProject, volumeID)
		s.Require().NoError(err)
		s.Equal(volumedomain.StateAvailable, volume.State)
		s.Empty(volume.Attachment.ID)
	}
}

// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/virt/block_device.py#L445-L470
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L2695-L2724
func (s *ServersSuite) TestBootFromImageCreatesBootableRootVolume() {
	s.addSubnet(serverNetwork, "10.20.0.0/24")

	created, err := s.servers.Create(s.request("req-create"), volumeBoot("web", app.BlockDevice{
		SourceType: app.BlockDeviceSourceImage, UUID: serverImage, BootIndex: bootIndex(0), VolumeSize: 2,
		DeleteOnTermination: true,
	}))

	s.Require().NoError(err)
	detail, err := s.servers.Get(serverProject, created.Server.ID)
	s.Require().NoError(err)
	s.Equal(domain.StatusActive, detail.Status())
	s.Empty(detail.ImageID)
	s.Require().Len(detail.AttachedVolumeIDs, 1)
	rootID := detail.AttachedVolumeIDs[0]
	root, err := s.volumes.Get(serverProject, rootID)
	s.Require().NoError(err)
	s.Equal(volumedomain.StateInUse, root.State)
	s.Equal("/dev/vda", root.Attachment.Device)
	s.Equal(2, root.Size)
	s.True(root.Bootable)
	s.True(root.ImageMetadataReady)
	s.Equal(serverImage, root.ImageSource.ID)
	s.Equal(volumedomain.NullableText{Valid: true}, root.Name)

	s.Require().NoError(s.servers.Delete(s.request("req-delete"), created.Server.ID))

	_, err = s.volumes.Get(serverProject, rootID)
	s.ErrorIs(err, appvolume.ErrVolumeNotFound)
}

// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L1743-L1757
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L745-L746
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L826-L869
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L1985-L2074
func (s *ServersSuite) TestBootFromVolumeRejectsRequestsInNovaOrder() {
	s.addSubnet(serverNetwork, "10.20.0.0/24")
	rootID := s.bootableVolume()
	busyID := s.bootableVolume()
	_, err := s.volumes.Attach(serverProject, "fixture", busyID, "")
	s.Require().NoError(err)
	largeImage, err := s.images.Create(serverProject, appimage.CreateOptions{
		Name: "large", ContainerFormat: "bare", DiskFormat: "qcow2", MinDiskGiB: 2,
	})
	s.Require().NoError(err)
	s.Require().NoError(s.images.Upload(serverProject, largeImage.ID, "image"))
	queued, err := s.images.Create(serverProject, appimage.CreateOptions{Name: "queued"})
	s.Require().NoError(err)
	volume := func(id string, index *int) app.BlockDevice {
		return app.BlockDevice{SourceType: app.BlockDeviceSourceVolume, UUID: id, BootIndex: index}
	}
	image := func(id string, size int) app.BlockDevice {
		return app.BlockDevice{SourceType: app.BlockDeviceSourceImage, UUID: id, BootIndex: bootIndex(0), VolumeSize: size}
	}
	withGroup := func(spec app.ServerCreate) app.ServerCreate {
		spec.SecurityGroups = []string{"missing"}
		return spec
	}
	cases := []struct {
		name    string
		spec    app.ServerCreate
		want    error
		message string
	}{
		{"unknown boot volume", volumeBoot("web", volume(missingVolume, bootIndex(0))), app.ErrBlockDeviceVolume,
			"Block Device Mapping is Invalid: failed to get volume " + missingVolume + "."},
		{"volume not bootable before security groups", withGroup(volumeBoot("web", volume(serverVolume, bootIndex(0)))),
			app.ErrVolumeNotBootable, "Block Device " + serverVolume + " is not bootable."},
		{"unknown boot image", volumeBoot("web", image(missingVolume, 1)), app.ErrBlockDeviceImage,
			"Block Device Mapping is Invalid: failed to get image " + missingVolume + "."},
		{"boot sequence after security groups", withGroup(volumeBoot("web", volume(rootID, nil))),
			app.ErrSecurityGroupNotFound, "Security group missing not found."},
		{"inactive image", volumeBoot("web", image(queued.ID, 1)), app.ErrImageNotActive,
			"Image None is not active."},
		{"volume below image min disk", volumeBoot("web", image(largeImage.ID, 1)), app.ErrVolumeSmallerThanMinDisk,
			"Volume is smaller than the minimum size specified in image metadata. Volume size is 1073741824 bytes, " +
				"minimum size is 2147483648 bytes."},
		{"unknown volume outside boot sequence", volumeBoot("web", volume(missingVolume, bootIndex(1))),
			app.ErrBlockDeviceVolumeNotFound, "Volume " + missingVolume + " could not be found."},
		{"no boot device", volumeBoot("web", volume(rootID, bootIndex(-1))), app.ErrBootSequence,
			"Block Device Mapping is Invalid: Boot sequence for the instance and image/block device mapping " +
				"combination is not valid."},
		{"image without size", volumeBoot("web", image(serverImage, 0)), app.ErrBlockDeviceSizeRequired,
			"Images with destination_type 'volume' need to have a non-zero size specified"},
		{"volume in use", volumeBoot("web", volume(busyID, bootIndex(0))), app.ErrVolumeNotReservable,
			"Invalid volume: Invalid input received: Invalid volume: Volume " + busyID + " status must be " +
				"available or downloading to reserve, but the current status is in-use. (HTTP 400)"},
	}
	for _, test := range cases {
		s.Run(test.name, func() {
			_, err := s.servers.Create(s.request("req"), test.spec)

			s.ErrorIs(err, test.want)
			s.EqualError(err, test.message)
		})
	}
	s.Len(s.servers.List(serverProject, app.ListFilter{}), 1)
	root, err := s.volumes.Get(serverProject, rootID)
	s.Require().NoError(err)
	s.Equal(volumedomain.StateAvailable, root.State)
	busy, err := s.volumes.Get(serverProject, busyID)
	s.Require().NoError(err)
	s.Equal("fixture", busy.Attachment.ServerID)
	s.Len(s.volumes.List(serverProject, appvolume.ListFilter{}), 3)
}

// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/manager.py#L2594-L2609
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/manager.py#L3340-L3368
func (s *ServersSuite) TestAbortedVolumeBuildReleasesRootVolume() {
	kept := s.bootableVolume()
	deleted := s.bootableVolume()

	keptServer, keptErr := s.servers.Create(s.request("req-kept"), volumeBoot("kept", app.BlockDevice{
		SourceType: app.BlockDeviceSourceVolume, UUID: kept, BootIndex: bootIndex(0),
	}))
	deletedServer, deletedErr := s.servers.Create(s.request("req-deleted"), volumeBoot("deleted", app.BlockDevice{
		SourceType: app.BlockDeviceSourceVolume, UUID: deleted, BootIndex: bootIndex(0), DeleteOnTermination: true,
	}))

	s.Require().NoError(keptErr)
	s.Require().NoError(deletedErr)
	for _, serverID := range []string{keptServer.Server.ID, deletedServer.Server.ID} {
		detail, err := s.servers.Get(serverProject, serverID)
		s.Require().NoError(err)
		s.Equal(domain.StatusError, detail.Status())
		s.Empty(detail.AttachedVolumeIDs)
	}
	volume, err := s.volumes.Get(serverProject, kept)
	s.Require().NoError(err)
	s.Equal(volumedomain.StateAvailable, volume.State)
	_, err = s.volumes.Get(serverProject, deleted)
	s.ErrorIs(err, appvolume.ErrVolumeNotFound)
}

// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/conductor/manager.py#L1650-L1655
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/manager.py#L2963-L2969
func (s *ServersSuite) TestHeldVolumeBuildsPrepareRootVolumeOnCompletion() {
	s.addSubnet(serverNetwork, "10.20.0.0/24")
	rootID := s.bootableVolume()
	s.compute.SetHoldBuilds(true)
	fromImage := app.BlockDevice{
		SourceType: app.BlockDeviceSourceImage, UUID: serverImage, BootIndex: bootIndex(0), VolumeSize: 1,
	}
	reserved, err := s.servers.Create(s.request("req-reserved"), volumeBoot("reserved", app.BlockDevice{
		SourceType: app.BlockDeviceSourceVolume, UUID: rootID, BootIndex: bootIndex(0), DeleteOnTermination: true,
	}))
	s.Require().NoError(err)
	completed, err := s.servers.Create(s.request("req-completed"), volumeBoot("completed", fromImage))
	s.Require().NoError(err)
	aborted, err := s.servers.Create(s.request("req-aborted"), volumeBoot("aborted", fromImage))
	s.Require().NoError(err)

	root, err := s.volumes.Get(serverProject, rootID)
	s.Require().NoError(err)
	s.Equal(volumedomain.StateReserved, root.State)
	s.Equal("/dev/vda", root.Attachment.Device)
	s.Len(s.volumes.List(serverProject, appvolume.ListFilter{}), 2)

	s.start = s.start.Add(time.Minute)
	s.Require().NoError(s.servers.FinishBuild(reserved.Server.ID, true))
	s.Require().NoError(s.servers.FinishBuild(completed.Server.ID, false))
	s.Require().NoError(s.images.SetDeactivated(serverImage, true))
	s.Require().NoError(s.servers.FinishBuild(aborted.Server.ID, false))

	root, err = s.volumes.Get(serverProject, rootID)
	s.Require().NoError(err)
	s.Equal(volumedomain.StateReserved, root.State)
	active, err := s.servers.Get(serverProject, completed.Server.ID)
	s.Require().NoError(err)
	s.Equal(domain.StatusActive, active.Status())
	s.Require().Len(active.AttachedVolumeIDs, 1)
	created, err := s.volumes.Get(serverProject, active.AttachedVolumeIDs[0])
	s.Require().NoError(err)
	s.Equal(volumedomain.StateInUse, created.State)
	s.True(created.Bootable)
	errored, err := s.servers.Get(serverProject, aborted.Server.ID)
	s.Require().NoError(err)
	s.Equal(&domain.Fault{
		Code: 500, Message: "Build of instance " + aborted.Server.ID + " aborted: Failure prepping block device.",
		CreatedAt: s.start,
	}, errored.Fault)
	s.Empty(errored.Ports)
	s.Empty(errored.AttachedVolumeIDs)
	s.Len(s.volumes.List(serverProject, appvolume.ListFilter{}), 3)
	s.Len(s.network.ListPorts(serverProject, appnetwork.PortListFilter{
		DeviceOwners: []string{networkdomain.DeviceOwnerCompute},
	}), 2)

	s.Require().NoError(s.servers.Delete(s.request("req-delete"), reserved.Server.ID))

	_, err = s.volumes.Get(serverProject, rootID)
	s.ErrorIs(err, appvolume.ErrVolumeNotFound)
}
