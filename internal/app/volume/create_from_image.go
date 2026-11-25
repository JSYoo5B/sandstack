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
	projectID, imageID, name, description string,
	size int,
	metadata map[string]string,
) (domain.Volume, error) {
	if s.seed.ProjectID != projectID {
		return domain.Volume{}, ErrVolumeNotFound
	}
	if size <= 0 {
		return domain.Volume{}, ErrInvalidVolumeSize
	}
	if s.images == nil {
		return domain.Volume{}, ErrVolumeSourceImageUnavailable
	}
	image, err := s.images.Get(projectID, imageID)
	if err != nil || imageID == "" || image.ID != imageID || image.ProjectID != projectID || image.Status() != "active" {
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
	return s.createVolume(name, description, size, metadata, volumeSource{image: source})
}
