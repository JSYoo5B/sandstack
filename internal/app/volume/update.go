package volume

import domain "github.com/JSYoo5B/sandstack/internal/domain/volume"

type VolumeUpdate struct {
	Name        *domain.NullableText
	Description *domain.NullableText
}

func (s *Service) Update(projectID, volumeID string, changes VolumeUpdate) (domain.Volume, error) {
	if s.seed.ProjectID != projectID {
		return domain.Volume{}, ErrVolumeNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	volume, ok := s.volumes[volumeID]
	if !ok {
		return domain.Volume{}, ErrVolumeNotFound
	}
	before := volume
	if changes.Name != nil {
		volume.Name = *changes.Name
	}
	if changes.Description != nil {
		volume.Description = *changes.Description
	}
	if volume.Name != before.Name || volume.Description != before.Description {
		volume.UpdatedAt = s.now().UTC()
		s.volumes[volumeID] = volume
	}
	return volume, nil
}
