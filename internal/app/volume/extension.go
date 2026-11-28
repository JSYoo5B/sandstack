package volume

import (
	"errors"

	domain "github.com/JSYoo5B/sandstack/internal/domain/volume"
)

var ErrInvalidExtensionSize = errors.New("new volume size must be larger than the current size")

// StartExtension records the requested size without changing the current size.
func (s *Service) StartExtension(projectID, volumeID string, newSize int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	volume, ok := s.ownedVolume(projectID, volumeID)
	if !ok {
		return ErrVolumeNotFound
	}
	if newSize <= 0 || newSize <= volume.Size {
		return ErrInvalidExtensionSize
	}
	if volume.State != domain.StateAvailable {
		return ErrVolumeStateConflict
	}
	s.updateState(volume, domain.StateExtending)
	s.extensions[volumeID] = newSize
	return nil
}

// FinishExtension applies one outcome to a pending extension.
func (s *Service) FinishExtension(volumeID string, failed bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	volume, ok := s.volumes[volumeID]
	if !ok {
		return ErrVolumeNotFound
	}
	newSize, pending := s.extensions[volumeID]
	if !pending || volume.State != domain.StateExtending {
		return ErrVolumeStateConflict
	}
	state := domain.StateErrorExtending
	if !failed {
		state = domain.StateAvailable
		volume.Size = newSize
	}
	s.updateState(volume, state)
	delete(s.extensions, volumeID)
	return nil
}
