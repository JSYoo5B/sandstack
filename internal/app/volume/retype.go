package volume

import (
	"errors"

	domain "github.com/JSYoo5B/sandstack/internal/domain/volume"
)

var ErrVolumeTypeUnchanged = errors.New("the requested volume type must differ from the current type")

// Retype completes immediately because the fixture types have equal extra specs.
func (s *Service) Retype(projectID, volumeID, selector string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	volume, ok := s.ownedVolume(projectID, volumeID)
	if !ok {
		return ErrVolumeNotFound
	}
	volumeType, ok := findVolumeType(selector)
	if !ok {
		return ErrVolumeTypeNotFound
	}
	if volume.State != domain.StateAvailable && volume.State != domain.StateInUse {
		return ErrVolumeStateConflict
	}
	if effectiveVolumeTypeID(volume.TypeID) == volumeType.ID {
		return ErrVolumeTypeUnchanged
	}
	volume.TypeID = volumeType.ID
	volume.UpdatedAt = s.now().UTC()
	s.volumes[volumeID] = volume
	return nil
}
