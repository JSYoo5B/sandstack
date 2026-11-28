package compute

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	domain "github.com/JSYoo5B/sandstack/internal/domain/compute"
	imagedomain "github.com/JSYoo5B/sandstack/internal/domain/image"
)

var ErrInvalidRebuild = errors.New("the rebuild request is invalid")

var trueStrings = []string{"1", "t", "true", "on", "y", "yes"}

type RebuildError struct {
	Kind    error
	Message string
}

func (e *RebuildError) Error() string { return e.Message }

func (e *RebuildError) Unwrap() error { return e.Kind }

func rebuildError(kind error, format string, args ...any) error {
	return &RebuildError{Kind: kind, Message: fmt.Sprintf(format, args...)}
}

type ServerRebuild struct {
	ImageID        string
	Name           *string
	AdminPass      *string
	Metadata       map[string]string
	AutoDiskConfig *bool
}

type RebuiltServer struct {
	Server    ServerDetail
	AdminPass string
}

func (s *Servers) Rebuild(request ActionRequest, serverID string, spec ServerRebuild) (RebuiltServer, error) {
	detail, err := s.rebuild(request, serverID, spec)
	if err != nil {
		return RebuiltServer{}, err
	}
	var password string
	if spec.AdminPass != nil {
		password = *spec.AdminPass
	} else {
		password = s.newPassword()
	}
	return RebuiltServer{Server: s.describe(detail), AdminPass: password}, nil
}

func (s *Servers) rebuild(request ActionRequest, serverID string, spec ServerRebuild) (ServerDetail, error) {
	projectID := request.ProjectID
	s.compute.mu.Lock()
	defer s.compute.mu.Unlock()

	record, ok := s.compute.find(projectID, serverID)
	if !ok {
		return ServerDetail{}, ErrServerNotFound
	}
	server := &record.server
	if server.Locked {
		return ServerDetail{}, &LockedError{ServerID: server.ID}
	}
	if err := checkInstanceState("rebuild", *server,
		domain.StatusActive, domain.StatusShutoff, domain.StatusError); err != nil {
		return ServerDetail{}, err
	}
	image, err := s.images.Get(projectID, spec.ImageID)
	if err != nil {
		return ServerDetail{}, rebuildError(ErrImageNotFound, "Cannot find image for rebuild")
	}
	if spec.AutoDiskConfig != nil && *spec.AutoDiskConfig && autoDiskConfigDisabled(image.Properties) {
		return ServerDetail{}, rebuildError(ErrInvalidRebuild,
			"Requested image %s has automatic disk resize disabled.", image.ID)
	}
	if server.BootVolume != nil && s.rootVolumeImage(projectID, *server.BootVolume) != spec.ImageID {
		return ServerDetail{}, rebuildError(ErrInvalidRebuild,
			"Image %s is unacceptable: Unable to rebuild with a different image for a volume-backed server.",
			spec.ImageID)
	}
	if len(spec.Metadata) > maxMetadataItems {
		return ServerDetail{}, rebuildError(ErrServerMetadataQuota,
			"Maximum number of metadata items exceeds %d", maxMetadataItems)
	}
	flavor, err := s.compute.GetFlavor(projectID, server.FlavorID)
	if err != nil {
		return ServerDetail{}, err
	}
	if err := checkRebuildImage(image, flavor, server.BootVolume); err != nil {
		return ServerDetail{}, err
	}

	if server.BootVolume == nil {
		server.ImageID = image.ID
	}
	imageMeta := imageMetaFromImage(image, flavor.DiskGiB, image.ID)
	server.ImageMeta = &imageMeta
	if spec.AutoDiskConfig != nil {
		server.AutoDiskConfig = *spec.AutoDiskConfig
	} else {
		server.AutoDiskConfig = boolFromString(image.Properties["auto_disk_config"])
	}
	if spec.Name != nil {
		server.Name = *spec.Name
	}
	if spec.Metadata != nil {
		server.Metadata = copyMetadata(spec.Metadata)
	}
	if server.Status() != domain.StatusShutoff {
		server.State = domain.StatusActive
	}
	server.Fault = nil
	server.UpdatedAt = s.compute.now().UTC()
	s.compute.recordAction(record, request, domain.ActionRebuild)
	return s.compute.liveDetail(record), nil
}

func (s *Servers) rootVolumeImage(projectID string, bootVolume domain.BootVolume) string {
	volume, err := s.volumes.Lookup(projectID, bootVolume.VolumeID)
	if err != nil || !volume.ImageMetadataReady {
		return ""
	}
	return volume.ImageSource.ID
}

func checkRebuildImage(image imagedomain.Image, flavor domain.Flavor, bootVolume *domain.BootVolume) error {
	if image.Status() != "active" {
		return rebuildError(ErrImageNotActive, "Image %s is not active.", image.ID)
	}
	if flavor.RAMMiB < image.MinRAMMiB {
		return rebuildError(ErrInvalidRebuild, "Flavor's memory is too small for requested image.")
	}
	minDisk := int64(image.MinDiskGiB) * gibibyte
	if bootVolume != nil {
		size := int64(bootVolume.Size) * gibibyte
		if size != 0 && minDisk > size {
			return rebuildError(ErrInvalidRebuild,
				"Volume is smaller than the minimum size specified in image metadata. "+
					"Volume size is %d bytes, minimum size is %d bytes.", size, minDisk)
		}
		return nil
	}
	size := int64(flavor.DiskGiB) * gibibyte
	if size == 0 {
		return nil
	}
	if image.Size > size {
		return rebuildError(ErrInvalidRebuild,
			"Flavor's disk is too small for requested image. Flavor disk is %d bytes, image is %d bytes.",
			size, image.Size)
	}
	if minDisk > size {
		return rebuildError(ErrInvalidRebuild,
			"Flavor's disk is smaller than the minimum size specified in image metadata. "+
				"Flavor disk is %d bytes, minimum size is %d bytes.", size, minDisk)
	}
	return nil
}

func autoDiskConfigDisabled(properties map[string]string) bool {
	return strings.ToLower(strings.TrimSpace(properties["auto_disk_config"])) == "disabled"
}

func boolFromString(value string) bool {
	return slices.Contains(trueStrings, strings.ToLower(strings.TrimSpace(value)))
}
