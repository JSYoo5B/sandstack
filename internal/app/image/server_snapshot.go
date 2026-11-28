package image

import (
	"maps"

	domain "github.com/JSYoo5B/sandstack/internal/domain/image"
)

func (s *Service) CreateSnapshot(projectID string, template domain.Image) (domain.Image, error) {
	template.Visibility = domain.VisibilityPrivate
	return s.createQueued(projectID, template)
}

func (s *Service) createQueued(projectID string, template domain.Image) (domain.Image, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	id, err := s.allocateID()
	if err != nil {
		return domain.Image{}, err
	}
	now := s.now().UTC()
	image := domain.Image{
		ID:              id,
		ProjectID:       projectID,
		Visibility:      template.Visibility,
		Name:            template.Name,
		ContainerFormat: template.ContainerFormat,
		DiskFormat:      template.DiskFormat,
		MinDiskGiB:      template.MinDiskGiB,
		MinRAMMiB:       template.MinRAMMiB,
		Properties:      maps.Clone(template.Properties),
		CreatedAt:       now,
		UpdatedAt:       now,
		Queued:          true,
	}
	s.images[id] = image
	return snapshot(image), nil
}

func (s *Service) CompleteSnapshot(
	projectID, imageID, sourceImageID, containerFormat, diskFormat string, properties map[string]string,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	image, ok := s.images[imageID]
	if !ok || projectID != image.ProjectID {
		return ErrImageNotFound
	}
	if !image.Queued || image.Deactivated {
		return ErrImageUploadState
	}
	data := s.payloads[sourceImageID]
	image.ContainerFormat = containerFormat
	image.DiskFormat = diskFormat
	image.Properties = maps.Clone(image.Properties)
	if image.Properties == nil {
		image.Properties = map[string]string{}
	}
	maps.Copy(image.Properties, properties)
	image.Queued = false
	image.Size = int64(len(data))
	image.Checksum = payloadChecksum(data)
	image.UpdatedAt = s.now().UTC()
	s.images[imageID] = image
	s.payloads[imageID] = data
	return nil
}
