package image

import (
	"errors"
	"slices"

	domain "github.com/JSYoo5B/sandstack/internal/domain/image"
)

var ErrImageFormat = errors.New("image container or disk format is not supported")

var (
	supportedContainerFormats = []string{"ami", "ari", "aki", "bare", "ovf", "ova", "docker"}
	supportedDiskFormats      = []string{"ami", "ari", "aki", "vhd", "vmdk", "raw", "qcow2", "vdi", "iso"}
)

func (s *Service) CreateVolumeImage(projectID string, template domain.Image) (domain.Image, error) {
	if template.ContainerFormat != "" && !slices.Contains(supportedContainerFormats, template.ContainerFormat) {
		return domain.Image{}, ErrImageFormat
	}
	if template.DiskFormat != "" && !slices.Contains(supportedDiskFormats, template.DiskFormat) {
		return domain.Image{}, ErrImageFormat
	}
	return s.CreateSnapshot(projectID, template)
}

func (s *Service) UploadImageData(projectID, imageID, sourceImageID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	image, err := s.checkUpload(projectID, imageID)
	if err != nil {
		return err
	}
	data := s.payloads[sourceImageID]
	image.Queued = false
	image.Size = int64(len(data))
	image.Checksum = payloadChecksum(data)
	image.UpdatedAt = s.now().UTC()
	s.images[imageID] = image
	s.payloads[imageID] = data
	return nil
}
