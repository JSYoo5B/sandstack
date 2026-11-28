package compute

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	domain "github.com/JSYoo5B/sandstack/internal/domain/compute"
	imagedomain "github.com/JSYoo5B/sandstack/internal/domain/image"
	volumedomain "github.com/JSYoo5B/sandstack/internal/domain/volume"
)

const (
	snapshotImageType     = "snapshot"
	snapshotDiskFormat    = imagedomain.DiskFormatQCOW2
	rootDeviceName        = "/dev/vda"
	snapshotDiskBus       = "virtio"
	snapshotDeviceType    = "disk"
	baseImageRefProperty  = "base_image_ref"
	nonInheritableImageNS = "os_glance"
)

var (
	ErrImageMetadataQuota = errors.New("Image metadata limit exceeded")
	ErrSnapshotNotPending = errors.New("the server has no pending snapshot")
	ErrImageBlockDevices  = errors.New("booting from an image with a block device mapping is not supported")
)

var (
	skippedImageProperties = []string{
		"mappings", "block_device_mapping", "img_mappings", "img_block_device_mapping",
	}
	nonInheritableImageProperties = []string{
		"cache_in_nova", "bittorrent",
		"cinder_encryption_key_id", "cinder_encryption_key_deletion_policy",
		"img_signature", "img_signature_hash_method", "img_signature_key_type", "img_signature_certificate_uuid",
	}
	snapshotMappingFields = []string{
		"boot_index", "delete_on_termination", "destination_type", "device_name", "device_type", "disk_bus",
		"encrypted", "encryption_format", "encryption_options", "encryption_secret_uuid", "guest_format",
		"image_id", "no_device", "snapshot_id", "source_type", "tag", "volume_id", "volume_size", "volume_type",
	}
)

type InvalidStateError struct {
	Message string
}

func (e *InvalidStateError) Error() string { return e.Message }

type snapshotImages interface {
	CreateSnapshot(projectID string, template imagedomain.Image) (imagedomain.Image, error)
	CompleteSnapshot(
		projectID, imageID, sourceImageID, containerFormat, diskFormat string, properties map[string]string,
	) error
	Upload(projectID, imageID, data string) error
	Delete(projectID, imageID string) error
}

func (s *Service) SetHoldImages(enabled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.holdImages = enabled
}

func (s *Servers) CreateImage(
	request ActionRequest, serverID, name string, metadata map[string]string,
) (string, error) {
	projectID := request.ProjectID
	s.compute.mu.Lock()
	defer s.compute.mu.Unlock()

	record, ok := s.compute.find(projectID, serverID)
	if !ok {
		return "", ErrServerNotFound
	}
	if len(metadata) > maxMetadataItems {
		return "", ErrImageMetadataQuota
	}
	server := &record.server
	if err := checkSnapshotState(*server); err != nil {
		return "", err
	}
	imageMeta := s.snapshotImageMeta(projectID, *server)
	if server.BootVolume != nil {
		return s.snapshotVolumeBacked(request, record, name, metadata, imageMeta)
	}

	properties := inheritedProperties(imageMeta.Properties)
	properties["instance_uuid"] = server.ID
	properties["user_id"] = request.UserID
	properties["image_type"] = snapshotImageType
	maps.Copy(properties, metadata)
	image, err := s.images.CreateSnapshot(projectID, imagedomain.Image{
		Name:       name,
		MinDiskGiB: imageMeta.MinDiskGiB,
		MinRAMMiB:  imageMeta.MinRAMMiB,
		Properties: properties,
	})
	if err != nil {
		return "", err
	}
	server.TaskState = domain.TaskImageSnapshotPending
	server.UpdatedAt = s.compute.now().UTC()
	s.compute.recordAction(record, request, domain.ActionCreateImage)
	if s.compute.holdImages {
		server.PendingImageID = image.ID
		return image.ID, nil
	}
	s.completeImage(server, image.ID, imageMeta.ContainerFormat)
	return image.ID, nil
}

func (s *Servers) FinishImage(serverID string, failed bool) error {
	s.compute.mu.Lock()
	defer s.compute.mu.Unlock()

	record, ok := s.compute.servers[serverID]
	if !ok {
		return ErrServerNotFound
	}
	server := &record.server
	imageID := server.PendingImageID
	if imageID == "" {
		return ErrSnapshotNotPending
	}
	if failed {
		_ = s.images.Delete(server.ProjectID, imageID)
		server.TaskState = ""
		server.PendingImageID = ""
		server.UpdatedAt = s.compute.now().UTC()
		return nil
	}
	s.completeImage(server, imageID, s.snapshotImageMeta(server.ProjectID, *server).ContainerFormat)
	return nil
}

func (s *Servers) completeImage(server *domain.Server, imageID, containerFormat string) {
	if containerFormat == "" {
		containerFormat = imagedomain.ContainerFormatBare
	}
	_ = s.images.CompleteSnapshot(server.ProjectID, imageID, server.ImageID, containerFormat, snapshotDiskFormat,
		map[string]string{
			"image_location": "snapshot",
			"image_state":    "available",
			"owner_id":       server.ProjectID,
		})
	server.TaskState = ""
	server.PendingImageID = ""
	server.UpdatedAt = s.compute.now().UTC()
}

func (s *Servers) snapshotVolumeBacked(
	request ActionRequest, record *serverRecord, name string, metadata map[string]string, imageMeta domain.ImageMeta,
) (string, error) {
	server := record.server
	projectID := request.ProjectID
	properties := inheritedProperties(imageMeta.Properties)
	maps.Copy(properties, metadata)
	for _, key := range []string{"block_device_mapping", "bdm_v2", "root_device_name"} {
		delete(properties, key)
	}
	properties["root_device_name"] = rootDeviceName

	s.compute.recordAction(record, request, domain.ActionCreateImage)
	var mappings []string
	for _, attachment := range s.rootFirstAttachments(projectID, server) {
		volume, err := s.volumes.Lookup(projectID, attachment.VolumeID)
		if err != nil {
			return "", err
		}
		snapshot, err := s.volumes.CreateServerSnapshot(projectID, volume.ID, "snapshot for "+name)
		if err != nil {
			return "", err
		}
		mappings = append(mappings, snapshotMapping(snapshot.ID, attachment, volume, server.BootVolume))
	}
	if len(mappings) > 0 {
		properties["block_device_mapping"] = "[" + strings.Join(mappings, ", ") + "]"
		properties["bdm_v2"] = "True"
	}
	image, err := s.images.CreateSnapshot(projectID, imagedomain.Image{
		Name:            name,
		ContainerFormat: imagedomain.ContainerFormatBare,
		DiskFormat:      imagedomain.DiskFormatRaw,
		MinDiskGiB:      imageMeta.MinDiskGiB,
		MinRAMMiB:       imageMeta.MinRAMMiB,
		Properties:      properties,
	})
	if err != nil {
		return "", err
	}
	if err := s.images.Upload(projectID, image.ID, ""); err != nil {
		return "", err
	}
	return image.ID, nil
}

func (s *Servers) rootFirstAttachments(projectID string, server domain.Server) []volumedomain.Attachment {
	attachments := s.volumes.ListAttachments(projectID, server.ID)
	slices.SortStableFunc(attachments, func(a, b volumedomain.Attachment) int {
		aRoot := a.VolumeID == server.BootVolume.VolumeID
		bRoot := b.VolumeID == server.BootVolume.VolumeID
		switch {
		case aRoot == bRoot:
			return 0
		case aRoot:
			return -1
		default:
			return 1
		}
	})
	return attachments
}

func snapshotMapping(
	snapshotID string, attachment volumedomain.Attachment, volume volumedomain.Volume, root *domain.BootVolume,
) string {
	values := map[string]any{
		"source_type":      "snapshot",
		"destination_type": "volume",
		"snapshot_id":      snapshotID,
		"disk_bus":         snapshotDiskBus,
		"device_type":      snapshotDeviceType,
		"device_name":      attachment.Device,
		"volume_size":      volume.Size,
	}
	if root != nil && root.VolumeID == volume.ID {
		values["boot_index"] = 0
		values["delete_on_termination"] = root.DeleteOnTermination
	} else {
		values["delete_on_termination"] = false
	}
	fields := make([]string, len(snapshotMappingFields))
	for i, field := range snapshotMappingFields {
		encoded, _ := json.Marshal(values[field])
		fields[i] = fmt.Sprintf("%q: %s", field, encoded)
	}
	return "{" + strings.Join(fields, ", ") + "}"
}

func checkSnapshotState(server domain.Server) error {
	return checkInstanceState("createImage", server,
		domain.StatusActive, domain.StatusPaused, domain.StatusSuspended, domain.StatusShutoff)
}

func (s *Servers) snapshotImageMeta(projectID string, server domain.Server) domain.ImageMeta {
	if server.ImageMeta != nil {
		return *server.ImageMeta
	}
	flavor, _ := s.compute.GetFlavor(projectID, server.FlavorID)
	if image, err := s.images.Get(projectID, server.ImageID); err == nil {
		return imageMetaFromImage(image, flavor.DiskGiB, server.ImageID)
	}
	return domain.ImageMeta{
		MinDiskGiB: flavor.DiskGiB,
		Properties: map[string]string{baseImageRefProperty: server.ImageID},
	}
}

func (s *Servers) bootImageMeta(projectID string, spec ServerCreate, source bootSource, flavorDisk int) *domain.ImageMeta {
	if spec.ImageID != "" {
		image, err := s.images.Get(projectID, spec.ImageID)
		if err != nil {
			return nil
		}
		meta := imageMetaFromImage(image, flavorDisk, spec.ImageID)
		return &meta
	}
	if source.root == nil {
		return nil
	}
	if source.root.SourceType == BlockDeviceSourceImage {
		image, err := s.images.Get(projectID, source.root.UUID)
		if err != nil {
			return nil
		}
		meta := imageMetaFromImage(image, flavorDisk, "")
		return &meta
	}
	meta := domain.ImageMeta{MinDiskGiB: flavorDisk, Properties: map[string]string{baseImageRefProperty: ""}}
	if volume, err := s.volumes.Lookup(projectID, source.root.UUID); err == nil && volume.ImageMetadataReady {
		meta.MinRAMMiB = volume.ImageSource.MinRAMMiB
		meta.MinDiskGiB = max(volume.ImageSource.MinDiskGiB, flavorDisk)
	}
	return &meta
}

func imageMetaFromImage(image imagedomain.Image, flavorDisk int, imageRef string) domain.ImageMeta {
	properties := map[string]string{}
	for key, value := range image.Properties {
		if !slices.Contains(skippedImageProperties, key) {
			properties[key] = value
		}
	}
	if _, ok := properties[baseImageRefProperty]; !ok {
		properties[baseImageRefProperty] = imageRef
	}
	minDisk := max(image.MinDiskGiB, flavorDisk)
	if image.DiskFormat == "vhd" {
		minDisk = flavorDisk
	}
	return domain.ImageMeta{
		MinRAMMiB:       image.MinRAMMiB,
		MinDiskGiB:      minDisk,
		ContainerFormat: image.ContainerFormat,
		DiskFormat:      image.DiskFormat,
		Properties:      properties,
	}
}

func inheritedProperties(properties map[string]string) map[string]string {
	inherited := map[string]string{}
	for key, value := range properties {
		if slices.Contains(nonInheritableImageProperties, key) || strings.HasPrefix(key, nonInheritableImageNS) {
			continue
		}
		inherited[key] = value
	}
	return inherited
}
