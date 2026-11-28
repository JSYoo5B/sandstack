package volume

import (
	"errors"

	imagedomain "github.com/JSYoo5B/sandstack/internal/domain/image"
	domain "github.com/JSYoo5B/sandstack/internal/domain/volume"
)

const bytesPerGiB int64 = 1 << 30

var (
	ErrVolumeSourceImageUnavailable = errors.New("source image must be visible and active")
	ErrVolumeSourceImageSize        = errors.New("volume size is smaller than required by the source image")
)

type imageLookup interface {
	Get(projectID, imageID string) (imagedomain.Image, error)
}

// CreateFromImage captures the active source image's metadata for provisioning.
func (s *Service) CreateFromImage(
	projectID, userID, imageID, name, description string,
	size int,
	metadata map[string]string,
) (domain.Volume, error) {
	return s.createFromImage(owner{projectID, userID}, "", imageID, name, description, size, metadata)
}

func (s *Service) CreateServerVolume(projectID, userID, imageID string, size int) (domain.Volume, error) {
	volume, err := s.CreateVolume(projectID, userID, CreateRequest{ImageID: imageID, Size: size})
	if err != nil {
		return domain.Volume{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	stored := s.volumes[volume.ID]
	stored.Name = domain.NullableText{Valid: true}
	stored.Description = domain.NullableText{Valid: true}
	s.volumes[volume.ID] = stored
	return cloneVolume(stored), nil
}

func (s *Service) createFromImage(
	creator owner, typeID, imageID, name, description string,
	size int,
	metadata map[string]string,
) (domain.Volume, error) {
	if size <= 0 {
		return domain.Volume{}, ErrInvalidVolumeSize
	}
	if s.images == nil {
		return domain.Volume{}, ErrVolumeSourceImageUnavailable
	}
	image, err := s.images.Get(creator.projectID, imageID)
	if err != nil || imageID == "" || image.ID != imageID || image.ProjectID != creator.projectID ||
		image.Status() != "active" {
		return domain.Volume{}, ErrVolumeSourceImageUnavailable
	}
	minimumGiB := image.Size / bytesPerGiB
	if image.Size%bytesPerGiB > 0 {
		minimumGiB++
	}
	if int64(size) < minimumGiB || size < image.MinDiskGiB {
		return domain.Volume{}, ErrVolumeSourceImageSize
	}
	source := domain.ImageSource{
		ID:              image.ID,
		Name:            image.Name,
		Checksum:        image.Checksum,
		ContainerFormat: image.ContainerFormat,
		DiskFormat:      image.DiskFormat,
		Size:            image.Size,
		MinDiskGiB:      image.MinDiskGiB,
		MinRAMMiB:       image.MinRAMMiB,
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.createVolume(creator, name, description, size, metadata, volumeSource{image: source, typeID: typeID})
}
