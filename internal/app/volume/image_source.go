package volume

import domain "github.com/JSYoo5B/sandstack/internal/domain/volume"

// GetPublishedImageSource reads image provenance independently of detail faults.
func (s *Service) GetPublishedImageSource(projectID, volumeID string) (domain.ImageSource, error) {
	if s.seed.ProjectID != projectID {
		return domain.ImageSource{}, ErrVolumeNotFound
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	volume, ok := s.volumes[volumeID]
	if !ok {
		return domain.ImageSource{}, ErrVolumeNotFound
	}
	if !volume.ImageMetadataReady {
		return domain.ImageSource{}, nil
	}
	return volume.ImageSource, nil
}
