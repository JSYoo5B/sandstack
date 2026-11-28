package compute

import (
	"errors"
	"slices"

	domain "github.com/JSYoo5B/sandstack/internal/domain/compute"
	volumedomain "github.com/JSYoo5B/sandstack/internal/domain/volume"
)

const (
	BlockDeviceSourceVolume = "volume"
	BlockDeviceSourceImage  = "image"

	bytesPerGiB               int64 = 1 << 30
	blockDevicePrepareFailure       = "Failure prepping block device."
)

var (
	ErrBlockDeviceImage          = errors.New("block device image could not be read")
	ErrBlockDeviceVolume         = errors.New("block device volume could not be read")
	ErrBlockDeviceVolumeNotFound = errors.New("block device volume not found")
	ErrVolumeNotBootable         = errors.New("block device volume is not bootable")
	ErrBootSequence              = errors.New("block device boot sequence is invalid")
	ErrBlockDeviceSizeRequired   = errors.New("image block device requires a volume size")
	ErrVolumeSmallerThanMinDisk  = errors.New("volume is smaller than the image minimum disk")
	ErrVolumeNotReservable       = errors.New("volume cannot be reserved")
	ErrRootVolumeDetach          = errors.New("Cannot detach a root device volume")
)

type BlockDevice struct {
	SourceType          string
	UUID                string
	BootIndex           *int
	VolumeSize          int
	DeleteOnTermination bool
}

type bootSource struct {
	imageID    string
	label      string
	status     string
	minDiskGiB int
	root       *BlockDevice
}

func (d BlockDevice) boots() bool {
	return d.BootIndex != nil && *d.BootIndex == 0
}

func (s *Servers) volumeBootSource(projectID string, devices []BlockDevice) (bootSource, error) {
	for i := range devices {
		device := &devices[i]
		if !device.boots() {
			continue
		}
		if device.SourceType == BlockDeviceSourceImage {
			image, err := s.images.Get(projectID, device.UUID)
			if err != nil {
				return bootSource{}, createError(ErrBlockDeviceImage,
					"Block Device Mapping is Invalid: failed to get image %s.", device.UUID)
			}
			return bootSource{label: "None", status: image.Status(), minDiskGiB: image.MinDiskGiB, root: device}, nil
		}
		volume, err := s.volumes.Lookup(projectID, device.UUID)
		if err != nil {
			return bootSource{}, createError(ErrBlockDeviceVolume,
				"Block Device Mapping is Invalid: failed to get volume %s.", device.UUID)
		}
		if !volume.Bootable {
			return bootSource{}, createError(ErrVolumeNotBootable, "Block Device %s is not bootable.", device.UUID)
		}
		source := bootSource{label: "None", status: "active", root: device}
		if volume.ImageMetadataReady {
			source.minDiskGiB = volume.ImageSource.MinDiskGiB
		}
		return source, nil
	}
	return bootSource{}, nil
}

func checkBootImage(source bootSource) error {
	if source.status != "" && source.status != "active" {
		return createError(ErrImageNotActive, "Image %s is not active.", source.label)
	}
	if source.root == nil || source.root.VolumeSize == 0 {
		return nil
	}
	size := int64(source.root.VolumeSize) * bytesPerGiB
	minDisk := int64(source.minDiskGiB) * bytesPerGiB
	if minDisk > size {
		return createError(ErrVolumeSmallerThanMinDisk,
			"Volume is smaller than the minimum size specified in image metadata. Volume size is %d bytes, "+
				"minimum size is %d bytes.", size, minDisk)
	}
	return nil
}

func (s *Servers) validateBlockDevices(projectID string, devices []BlockDevice) error {
	volumes := map[string]volumedomain.Volume{}
	for _, device := range devices {
		if device.SourceType != BlockDeviceSourceVolume {
			continue
		}
		volume, err := s.volumes.Lookup(projectID, device.UUID)
		if err != nil {
			return createError(ErrBlockDeviceVolumeNotFound, "Volume %s could not be found.", device.UUID)
		}
		volumes[device.UUID] = volume
	}

	var indexes []int
	for _, device := range devices {
		if device.BootIndex != nil && *device.BootIndex >= 0 {
			indexes = append(indexes, *device.BootIndex)
		}
	}
	slices.Sort(indexes)
	for i, index := range indexes {
		if i != index {
			indexes = nil
			break
		}
	}
	if len(indexes) == 0 {
		return createError(ErrBootSequence, "Block Device Mapping is Invalid: Boot sequence for the instance "+
			"and image/block device mapping combination is not valid.")
	}

	for _, device := range devices {
		if device.SourceType == BlockDeviceSourceImage {
			if _, err := s.images.Get(projectID, device.UUID); err != nil {
				return createError(ErrBlockDeviceImage,
					"Block Device Mapping is Invalid: failed to get image %s.", device.UUID)
			}
			if device.VolumeSize == 0 {
				return createError(ErrBlockDeviceSizeRequired,
					"Images with destination_type 'volume' need to have a non-zero size specified")
			}
			continue
		}
		if volume := volumes[device.UUID]; volume.State != volumedomain.StateAvailable {
			return reservationError(device.UUID, volume.State)
		}
	}
	return nil
}

func reservationError(volumeID string, state volumedomain.State) error {
	return createError(ErrVolumeNotReservable,
		"Invalid volume: Invalid input received: Invalid volume: Volume %s status must be available or "+
			"downloading to reserve, but the current status is %s. (HTTP 400)", volumeID, state)
}

func newBootVolume(devices []BlockDevice) *domain.BootVolume {
	for _, device := range devices {
		if !device.boots() {
			continue
		}
		bootVolume := &domain.BootVolume{Size: device.VolumeSize, DeleteOnTermination: device.DeleteOnTermination}
		if device.SourceType == BlockDeviceSourceVolume {
			bootVolume.VolumeID = device.UUID
		} else {
			bootVolume.ImageID = device.UUID
		}
		return bootVolume
	}
	return nil
}

func (s *Servers) reserveBootVolume(projectID, serverID string, bootVolume *domain.BootVolume) error {
	if bootVolume == nil || bootVolume.VolumeID == "" {
		return nil
	}
	if _, err := s.volumes.ReserveRoot(projectID, serverID, bootVolume.VolumeID); err != nil {
		volume, lookupErr := s.volumes.Lookup(projectID, bootVolume.VolumeID)
		if lookupErr != nil {
			return createError(ErrBlockDeviceVolumeNotFound, "Volume %s could not be found.", bootVolume.VolumeID)
		}
		return reservationError(bootVolume.VolumeID, volume.State)
	}
	return nil
}

func (s *Servers) prepareBootVolume(projectID, serverID string, bootVolume *domain.BootVolume) string {
	if bootVolume == nil {
		return ""
	}
	if bootVolume.VolumeID == "" {
		volume, err := s.volumes.CreateServerVolume(projectID, bootVolume.ImageID, bootVolume.Size)
		if err != nil {
			return blockDevicePrepareFailure
		}
		if err := s.volumes.CompleteCreation(volume.ID); err != nil {
			return blockDevicePrepareFailure
		}
		if _, err := s.volumes.ReserveRoot(projectID, serverID, volume.ID); err != nil {
			return blockDevicePrepareFailure
		}
		bootVolume.VolumeID = volume.ID
	}
	if err := s.volumes.CompleteReservation(projectID, bootVolume.VolumeID); err != nil {
		return blockDevicePrepareFailure
	}
	return ""
}

func (s *Servers) releaseBootVolume(projectID, serverID string, bootVolume *domain.BootVolume) {
	if bootVolume == nil || bootVolume.VolumeID == "" {
		return
	}
	if err := s.volumes.Detach(projectID, serverID, bootVolume.VolumeID); err != nil {
		return
	}
	if bootVolume.DeleteOnTermination {
		_ = s.volumes.Delete(projectID, bootVolume.VolumeID)
	}
}
