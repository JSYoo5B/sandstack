package volume

import (
	"maps"

	domain "github.com/JSYoo5B/sandstack/internal/domain/volume"
)

// GetVolumeMetadata reads metadata independently of volume detail faults.
func (s *Service) GetVolumeMetadata(projectID, volumeID string) (map[string]string, error) {
	if s.seed.ProjectID != projectID {
		return nil, ErrVolumeNotFound
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	volume, ok := s.volumes[volumeID]
	if !ok {
		return nil, ErrVolumeNotFound
	}
	return copyMetadata(volume.Metadata), nil
}

// ReplaceVolumeMetadata changes only the metadata on the scoped volume.
func (s *Service) ReplaceVolumeMetadata(
	projectID, volumeID string,
	metadata map[string]string,
) (map[string]string, error) {
	if s.seed.ProjectID != projectID {
		return nil, ErrVolumeNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	volume, ok := s.volumes[volumeID]
	if !ok {
		return nil, ErrVolumeNotFound
	}
	// Stored metadata maps are immutable, including maps observed by list readers.
	volume.Metadata = copyMetadata(metadata)
	s.volumes[volumeID] = volume
	return maps.Clone(volume.Metadata), nil
}

func copyMetadata(metadata map[string]string) map[string]string {
	copied := make(map[string]string, len(metadata))
	maps.Copy(copied, metadata)
	return copied
}

func cloneVolume(volume domain.Volume) domain.Volume {
	volume.Metadata = maps.Clone(volume.Metadata)
	return volume
}
