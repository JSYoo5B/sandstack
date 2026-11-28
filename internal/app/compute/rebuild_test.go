package compute_test

import (
	"fmt"

	app "github.com/JSYoo5B/sandstack/internal/app/compute"
	domain "github.com/JSYoo5B/sandstack/internal/domain/compute"
	imagedomain "github.com/JSYoo5B/sandstack/internal/domain/image"
)

func (s *ServersSuite) imageWith(template imagedomain.Image, complete bool) string {
	template.Name = "rebuild"
	image, err := s.images.CreateSnapshot(serverProject, template)
	s.Require().NoError(err)
	if complete {
		s.Require().NoError(s.images.CompleteSnapshot(serverProject, image.ID, serverImage, "bare", "qcow2", nil))
	}
	return image.ID
}

// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/servers.py#L1264-L1336
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L3694-L3913
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L691-L706
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/instance_actions.py#L37
func (s *ServersSuite) TestRebuildReplacesImageNameAndMetadata() {
	s.addSubnet(serverNetwork, "10.20.0.0/24")
	created, err := s.servers.Create(s.request("req-create"), app.ServerCreate{
		Name: "web", ImageID: serverImage, FlavorID: app.FixtureFlavorID,
		Metadata: map[string]string{"role": "web", "tier": "silver"},
	})
	s.Require().NoError(err)
	serverID := created.Server.ID
	imageID := s.imageWith(imagedomain.Image{Properties: map[string]string{"auto_disk_config": "True"}}, true)
	name := "rebuilt"

	rebuilt, err := s.servers.Rebuild(s.request("req-rebuild"), serverID, app.ServerRebuild{
		ImageID: imageID, Name: &name, Metadata: map[string]string{"tier": "gold"},
	})

	s.Require().NoError(err)
	s.Equal("password-2", rebuilt.AdminPass)
	s.Equal(imageID, rebuilt.Server.ImageID)
	s.Equal("rebuilt", rebuilt.Server.Name)
	s.Equal(map[string]string{"tier": "gold"}, rebuilt.Server.Metadata)
	s.True(rebuilt.Server.AutoDiskConfig)
	s.Equal(domain.StatusActive, rebuilt.Server.Status())
	s.NotEmpty(rebuilt.Server.Networks)
	detail, err := s.servers.Get(serverProject, serverID)
	s.Require().NoError(err)
	s.Equal(imageID, detail.ImageID)
	s.Require().NotNil(detail.ImageMeta)
	s.Equal(imageID, detail.ImageMeta.Properties["base_image_ref"])
	s.Equal([]string{domain.ActionRebuild, domain.ActionCreate}, s.actionNames(serverID))
}

// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L3696-L3700
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/manager.py#L4082-L4094
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/manager.py#L2242-L2256
func (s *ServersSuite) TestRebuildKeepsStoppedStateAndRecoversError() {
	s.addSubnet(serverNetwork, "10.20.0.0/24")
	stopped := s.bootFromImage("stopped", serverImage)
	s.Require().NoError(s.compute.SetPower(s.request("req-stop"), stopped, false))
	failed := s.bootFromImage("failed", serverImage)
	s.Require().NoError(s.compute.SetFault(failed, true))
	password := "chosen"

	stoppedResult, stoppedErr := s.servers.Rebuild(s.request("req-rebuild-stopped"), stopped, app.ServerRebuild{
		ImageID: serverImage, AdminPass: &password,
	})
	failedResult, failedErr := s.servers.Rebuild(s.request("req-rebuild-failed"), failed, app.ServerRebuild{
		ImageID: serverImage,
	})

	s.Require().NoError(stoppedErr)
	s.Equal(domain.StatusShutoff, stoppedResult.Server.Status())
	s.Equal("chosen", stoppedResult.AdminPass)
	s.Equal("stopped", stoppedResult.Server.Name)
	s.Require().NoError(failedErr)
	s.Equal(domain.StatusActive, failedResult.Server.Status())
	s.Nil(failedResult.Server.Fault)
}

// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/servers.py#L1338-L1371
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L142-L180
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L247-L253
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L3747-L3750
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L531-L546
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L723-L894
func (s *ServersSuite) TestRebuildRejectsRequestsBeforeChangingState() {
	s.addSubnet(serverNetwork, "10.20.0.0/24")
	serverID := s.bootFromImage("web", serverImage)
	paused := s.bootFromImage("paused", serverImage)
	s.Require().NoError(s.compute.Pause(s.request("req-pause"), paused))
	locked := s.bootFromImage("locked", serverImage)
	s.Require().NoError(s.compute.Lock(s.request("req-lock"), locked))
	disabled := s.imageWith(imagedomain.Image{Properties: map[string]string{"auto_disk_config": "Disabled"}}, true)
	queued := s.imageWith(imagedomain.Image{}, false)
	hungry := s.imageWith(imagedomain.Image{MinRAMMiB: 1024}, true)
	large := s.imageWith(imagedomain.Image{MinDiskGiB: 2}, true)
	missing := "10000000-0000-4000-8000-999999999999"
	auto := true
	tooMany := map[string]string{}
	for i := range 129 {
		tooMany[fmt.Sprintf("key-%d", i)] = "value"
	}

	for _, tt := range []struct {
		name     string
		serverID string
		spec     app.ServerRebuild
		kind     error
		message  string
	}{
		{
			name: "unknown server", serverID: missing, spec: app.ServerRebuild{ImageID: serverImage},
			kind: app.ErrServerNotFound, message: app.ErrServerNotFound.Error(),
		},
		{
			name: "locked server", serverID: locked, spec: app.ServerRebuild{ImageID: missing},
			kind: app.ErrServerLocked, message: "Instance " + locked + " is locked",
		},
		{
			name: "paused server", serverID: paused, spec: app.ServerRebuild{ImageID: missing},
			message: "Cannot 'rebuild' instance " + paused + " while it is in vm_state paused",
		},
		{
			name: "unknown image", serverID: serverID, spec: app.ServerRebuild{ImageID: missing, Metadata: tooMany},
			kind: app.ErrImageNotFound, message: "Cannot find image for rebuild",
		},
		{
			name: "disabled disk config", serverID: serverID,
			spec: app.ServerRebuild{ImageID: disabled, AutoDiskConfig: &auto, Metadata: tooMany},
			kind: app.ErrInvalidRebuild, message: "Requested image " + disabled + " has automatic disk resize disabled.",
		},
		{
			name: "metadata quota", serverID: serverID, spec: app.ServerRebuild{ImageID: queued, Metadata: tooMany},
			kind: app.ErrServerMetadataQuota, message: "Maximum number of metadata items exceeds 128",
		},
		{
			name: "queued image", serverID: serverID, spec: app.ServerRebuild{ImageID: queued},
			kind: app.ErrImageNotActive, message: "Image " + queued + " is not active.",
		},
		{
			name: "image minimum memory", serverID: serverID, spec: app.ServerRebuild{ImageID: hungry},
			kind: app.ErrInvalidRebuild, message: "Flavor's memory is too small for requested image.",
		},
		{
			name: "image minimum disk", serverID: serverID, spec: app.ServerRebuild{ImageID: large},
			kind: app.ErrInvalidRebuild,
			message: "Flavor's disk is smaller than the minimum size specified in image metadata. " +
				"Flavor disk is 1073741824 bytes, minimum size is 2147483648 bytes.",
		},
	} {
		s.Run(tt.name, func() {
			_, err := s.servers.Rebuild(s.request("req-"+tt.name), tt.serverID, tt.spec)

			s.EqualError(err, tt.message)
			if tt.kind != nil {
				s.ErrorIs(err, tt.kind)
			}
		})
	}
	detail, err := s.servers.Get(serverProject, serverID)
	s.Require().NoError(err)
	s.Equal(serverImage, detail.ImageID)
	s.Equal([]string{domain.ActionCreate}, s.actionNames(serverID))
}

// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L3758-L3800
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L3860-L3863
func (s *ServersSuite) TestRebuildVolumeBackedServerKeepsRootImage() {
	s.addSubnet(serverNetwork, "10.20.0.0/24")
	created, err := s.servers.Create(s.request("req-create"), volumeBoot("web", app.BlockDevice{
		SourceType: app.BlockDeviceSourceVolume, UUID: s.bootableVolume(), BootIndex: bootIndex(0),
	}))
	s.Require().NoError(err)
	serverID := created.Server.ID
	other := s.imageWith(imagedomain.Image{}, true)

	_, otherErr := s.servers.Rebuild(s.request("req-other"), serverID, app.ServerRebuild{ImageID: other})
	rebuilt, sameErr := s.servers.Rebuild(s.request("req-same"), serverID, app.ServerRebuild{ImageID: serverImage})

	s.ErrorIs(otherErr, app.ErrInvalidRebuild)
	s.EqualError(otherErr, "Image "+other+
		" is unacceptable: Unable to rebuild with a different image for a volume-backed server.")
	s.Require().NoError(sameErr)
	s.Empty(rebuilt.Server.ImageID)
	s.Equal(domain.StatusActive, rebuilt.Server.Status())
	s.Equal([]string{domain.ActionRebuild, domain.ActionCreate}, s.actionNames(serverID))
}
