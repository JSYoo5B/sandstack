package compute_test

import (
	"fmt"
	"maps"

	app "github.com/JSYoo5B/sandstack/internal/app/compute"
	appimage "github.com/JSYoo5B/sandstack/internal/app/image"
	domain "github.com/JSYoo5B/sandstack/internal/domain/compute"
	imagedomain "github.com/JSYoo5B/sandstack/internal/domain/image"
	volumedomain "github.com/JSYoo5B/sandstack/internal/domain/volume"
)

const emptyChecksum = "d41d8cd98f00b204e9800998ecf8427e"

func (s *ServersSuite) bootFromImage(name, imageID string) string {
	created, err := s.servers.Create(s.request("req-"+name), app.ServerCreate{
		Name: name, ImageID: imageID, FlavorID: app.FixtureFlavorID,
	})
	s.Require().NoError(err)
	return created.Server.ID
}

func (s *ServersSuite) imageCount() int {
	return len(s.images.List(serverProject, appimage.ListFilter{}))
}

// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L3454-L3511
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/utils.py#L1253-L1335
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/utils.py#L536-L590
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/virt/libvirt/driver.py#L3184-L3206
func (s *ServersSuite) TestCreateImageSnapshotsImageBackedServer() {
	s.addSubnet(serverNetwork, "10.20.0.0/24")
	serverID := s.bootFromImage("web", serverImage)
	base, err := s.images.Get(serverProject, serverImage)
	s.Require().NoError(err)

	imageID, err := s.servers.CreateImage(s.request("req-image"), serverID, "web-snap",
		map[string]string{"purpose": "backup"})

	s.Require().NoError(err)
	image, err := s.images.Get(serverProject, imageID)
	s.Require().NoError(err)
	s.Equal("active", image.Status())
	s.Equal("web-snap", image.Name)
	s.Equal(serverProject, image.ProjectID)
	s.Equal(imagedomain.ContainerFormatBare, image.ContainerFormat)
	s.Equal(imagedomain.DiskFormatQCOW2, image.DiskFormat)
	s.Equal(1, image.MinDiskGiB)
	s.Equal(0, image.MinRAMMiB)
	s.Equal(base.Size, image.Size)
	s.Equal(base.Checksum, image.Checksum)
	s.Equal(map[string]string{
		"base_image_ref": serverImage,
		"instance_uuid":  serverID,
		"user_id":        serverUser,
		"image_type":     "snapshot",
		"purpose":        "backup",
		"image_location": "snapshot",
		"image_state":    "available",
		"owner_id":       serverProject,
	}, image.Properties)
	detail, err := s.servers.Get(serverProject, serverID)
	s.Require().NoError(err)
	s.Empty(detail.TaskState)
	action, err := s.compute.GetAction(serverProject, serverID, "req-image")
	s.Require().NoError(err)
	s.Equal(domain.ActionCreateImage, action.Name)
}

// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L2188-L2195
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/utils.py#L64-L77
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/utils.py#L1286-L1335
func (s *ServersSuite) TestCreateImageInheritsBootImageProperties() {
	s.addSubnet(serverNetwork, "10.20.0.0/24")
	custom, err := s.images.CreateSnapshot(serverProject, imagedomain.Image{
		Name: "custom", MinDiskGiB: 3, MinRAMMiB: 256, Properties: map[string]string{
			"hw_disk_bus": "scsi", "os_distro": "cirros", "os_glance_import_method": "web-download",
			"img_signature": "signature", "cache_in_nova": "True",
		},
	})
	s.Require().NoError(err)
	s.Require().NoError(s.images.CompleteSnapshot(serverProject, custom.ID, serverImage, "bare", "qcow2", nil))
	parentID := s.bootFromImage("parent", custom.ID)
	parentImageID, err := s.servers.CreateImage(s.request("req-parent"), parentID, "parent-snap",
		map[string]string{"hw_disk_bus": "virtio"})
	s.Require().NoError(err)
	childID := s.bootFromImage("child", parentImageID)

	childImageID, err := s.servers.CreateImage(s.request("req-child"), childID, "child-snap", nil)

	s.Require().NoError(err)
	parent, err := s.images.Get(serverProject, parentImageID)
	s.Require().NoError(err)
	s.Equal(3, parent.MinDiskGiB)
	s.Equal(256, parent.MinRAMMiB)
	s.Equal("virtio", parent.Properties["hw_disk_bus"])
	s.Equal("cirros", parent.Properties["os_distro"])
	s.Equal(custom.ID, parent.Properties["base_image_ref"])
	for _, key := range []string{"os_glance_import_method", "img_signature", "cache_in_nova"} {
		s.NotContains(parent.Properties, key)
	}
	child, err := s.images.Get(serverProject, childImageID)
	s.Require().NoError(err)
	expected := maps.Clone(parent.Properties)
	expected["instance_uuid"] = childID
	s.Equal(expected, child.Properties)
}

// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L142-L180
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/manager.py#L219-L237
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/manager.py#L4583-L4697
func (s *ServersSuite) TestHeldImageStaysPendingUntilFinished() {
	s.addSubnet(serverNetwork, "10.20.0.0/24")
	serverID := s.bootFromImage("web", serverImage)
	s.compute.SetHoldImages(true)

	pendingID, err := s.servers.CreateImage(s.request("req-pending"), serverID, "pending", nil)

	s.Require().NoError(err)
	pending, err := s.images.Get(serverProject, pendingID)
	s.Require().NoError(err)
	s.Equal("queued", pending.Status())
	s.Empty(pending.ContainerFormat)
	s.Empty(pending.DiskFormat)
	detail, err := s.servers.Get(serverProject, serverID)
	s.Require().NoError(err)
	s.Equal(domain.TaskImageSnapshotPending, detail.TaskState)
	_, repeatErr := s.servers.CreateImage(s.request("req-repeat"), serverID, "repeat", nil)
	s.EqualError(repeatErr, fmt.Sprintf(
		"Cannot 'createImage' instance %s while it is in task_state image_snapshot_pending", serverID))
	s.ErrorIs(s.compute.Pause(s.request("req-pause"), serverID), app.ErrServerPowerState)

	s.Require().NoError(s.servers.FinishImage(serverID, false))

	completed, err := s.images.Get(serverProject, pendingID)
	s.Require().NoError(err)
	s.Equal("active", completed.Status())
	s.Equal("snapshot", completed.Properties["image_location"])
	detail, err = s.servers.Get(serverProject, serverID)
	s.Require().NoError(err)
	s.Empty(detail.TaskState)
	s.ErrorIs(s.servers.FinishImage(serverID, false), app.ErrSnapshotNotPending)
	s.ErrorIs(s.servers.FinishImage("missing", false), app.ErrServerNotFound)

	failedID, err := s.servers.CreateImage(s.request("req-failed"), serverID, "failed", nil)
	s.Require().NoError(err)
	s.Require().NoError(s.servers.FinishImage(serverID, true))
	_, err = s.images.Get(serverProject, failedID)
	s.ErrorIs(err, appimage.ErrImageNotFound)
	detail, err = s.servers.Get(serverProject, serverID)
	s.Require().NoError(err)
	s.Empty(detail.TaskState)

	orphanID, err := s.servers.CreateImage(s.request("req-orphan"), serverID, "orphan", nil)
	s.Require().NoError(err)
	s.Require().NoError(s.servers.Delete(s.request("req-delete"), serverID))
	_, err = s.images.Get(serverProject, orphanID)
	s.ErrorIs(err, appimage.ErrImageNotFound)
}

// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/servers.py#L1425-L1488
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/common.py#L282-L289
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L3452-L3459
func (s *ServersSuite) TestCreateImageChecksServerQuotaAndState() {
	s.addSubnet(serverNetwork, "10.20.0.0/24")
	activeID := s.bootFromImage("active", serverImage)
	stoppedID := s.bootFromImage("stopped", serverImage)
	s.Require().NoError(s.compute.SetPower(s.request("req-stop"), stoppedID, false))
	s.Require().NoError(s.compute.Lock(s.request("req-lock"), stoppedID))
	s.compute.SetHoldBuilds(true)
	buildingID := s.bootFromImage("building", serverImage)
	failedID := s.bootFromImage("failed", serverImage)
	s.Require().NoError(s.servers.FinishBuild(failedID, true))
	tooMany := map[string]string{}
	for i := range 129 {
		tooMany[fmt.Sprintf("key-%d", i)] = "value"
	}
	before := s.imageCount()

	_, missingErr := s.servers.CreateImage(s.request("req-missing"), "missing", "snap", nil)
	_, otherErr := s.servers.CreateImage(app.ActionRequest{ProjectID: "other"}, activeID, "snap", nil)
	_, quotaErr := s.servers.CreateImage(s.request("req-quota"), activeID, "snap", tooMany)
	_, buildingErr := s.servers.CreateImage(s.request("req-building"), buildingID, "snap", nil)
	_, failedErr := s.servers.CreateImage(s.request("req-error"), failedID, "snap", nil)

	s.ErrorIs(missingErr, app.ErrServerNotFound)
	s.ErrorIs(otherErr, app.ErrServerNotFound)
	s.ErrorIs(quotaErr, app.ErrImageMetadataQuota)
	s.EqualError(buildingErr, fmt.Sprintf(
		"Cannot 'createImage' instance %s while it is in vm_state building", buildingID))
	s.EqualError(failedErr, fmt.Sprintf(
		"Cannot 'createImage' instance %s while it is in vm_state error", failedID))
	s.Equal(before, s.imageCount())
	_, err := s.compute.GetAction(serverProject, activeID, "req-quota")
	s.ErrorIs(err, app.ErrActionNotFound)

	lockedImageID, err := s.servers.CreateImage(s.request("req-locked"), stoppedID, "locked", nil)

	s.Require().NoError(err)
	image, err := s.images.Get(serverProject, lockedImageID)
	s.Require().NoError(err)
	s.Equal("active", image.Status())
}

// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L3513-L3653
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/block_device.py#L302-L313
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/image/glance.py#L523-L675
func (s *ServersSuite) TestCreateImageSnapshotsVolumeBackedServer() {
	s.addSubnet(serverNetwork, "10.20.0.0/24")
	rootID := s.bootableVolume()
	created, err := s.servers.Create(s.request("req-create"), volumeBoot("db", app.BlockDevice{
		SourceType: app.BlockDeviceSourceVolume, UUID: rootID, BootIndex: bootIndex(0), DeleteOnTermination: true,
	}))
	s.Require().NoError(err)
	serverID := created.Server.ID
	attachments := app.NewAttachments(s.compute, s.volumes)
	_, err = attachments.Attach(s.request("req-attach"), serverID, serverVolume, "")
	s.Require().NoError(err)

	imageID, err := s.servers.CreateImage(s.request("req-image"), serverID, "db-snap",
		map[string]string{"purpose": "backup"})

	s.Require().NoError(err)
	snapshots := map[string]volumedomain.Snapshot{}
	for _, snapshot := range s.volumes.ListSnapshots(serverProject) {
		snapshots[snapshot.VolumeID] = snapshot
	}
	s.Require().Len(snapshots, 2)
	rootSnapshot, extraSnapshot := snapshots[rootID], snapshots[serverVolume]
	s.Equal(volumedomain.NullableText{Value: "snapshot for db-snap", Valid: true}, rootSnapshot.Name)
	s.Equal(volumedomain.StateCreating, rootSnapshot.State)
	image, err := s.images.Get(serverProject, imageID)
	s.Require().NoError(err)
	s.Equal("active", image.Status())
	s.Equal("db-snap", image.Name)
	s.Equal(int64(0), image.Size)
	s.Equal(emptyChecksum, image.Checksum)
	s.Equal(imagedomain.ContainerFormatBare, image.ContainerFormat)
	s.Equal(imagedomain.DiskFormatRaw, image.DiskFormat)
	s.Equal(1, image.MinDiskGiB)
	mapping := `{"boot_index": %s, "delete_on_termination": %t, "destination_type": "volume", ` +
		`"device_name": "%s", "device_type": "disk", "disk_bus": "virtio", "encrypted": null, ` +
		`"encryption_format": null, "encryption_options": null, "encryption_secret_uuid": null, ` +
		`"guest_format": null, "image_id": null, "no_device": null, "snapshot_id": "%s", ` +
		`"source_type": "snapshot", "tag": null, "volume_id": null, "volume_size": 1, "volume_type": null}`
	s.Equal(map[string]string{
		"base_image_ref":   "",
		"purpose":          "backup",
		"root_device_name": "/dev/vda",
		"bdm_v2":           "True",
		"block_device_mapping": "[" +
			fmt.Sprintf(mapping, "0", true, "/dev/vda", rootSnapshot.ID) + ", " +
			fmt.Sprintf(mapping, "null", false, "/dev/vdb", extraSnapshot.ID) + "]",
	}, image.Properties)
	detail, err := s.servers.Get(serverProject, serverID)
	s.Require().NoError(err)
	s.Empty(detail.TaskState)
	action, err := s.compute.GetAction(serverProject, serverID, "req-image")
	s.Require().NoError(err)
	s.Equal(domain.ActionCreateImage, action.Name)

	_, bootErr := s.servers.Create(s.request("req-boot"), app.ServerCreate{
		Name: "restored", ImageID: imageID, FlavorID: app.FixtureFlavorID,
	})

	s.ErrorIs(bootErr, app.ErrImageBlockDevices)
}
