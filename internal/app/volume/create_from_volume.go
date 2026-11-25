package volume

import (
	"errors"

	domain "github.com/JSYoo5B/sandstack/internal/domain/volume"
)

var (
	ErrVolumeSourceNotFound    = errors.New("source volume not found")
	ErrVolumeSourceState       = errors.New("source volume must be available or in-use")
	ErrVolumeSmallerThanSource = errors.New("volume size must be at least the source volume size")
)

// CreateFromVolume accepts an owned source and captures its volume provenance.
// A zero size inherits the source size.
func (s *Service) CreateFromVolume(
	projectID, sourceVolumeID, name, description string,
	size int,
	metadata map[string]string,
) (domain.Volume, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	source, ok := s.volumes[sourceVolumeID]
	if !ok || sourceVolumeID == "" || source.ProjectID != projectID {
		return domain.Volume{}, ErrVolumeSourceNotFound
	}
	if source.State != domain.StateAvailable && source.State != domain.StateInUse {
		return domain.Volume{}, ErrVolumeSourceState
	}
	if size == 0 {
		size = source.Size
	}
	if size <= 0 {
		return domain.Volume{}, ErrInvalidVolumeSize
	}
	if size < source.Size {
		return domain.Volume{}, ErrVolumeSmallerThanSource
	}
	provenance := volumeSource{volumeID: source.ID, bootable: source.Bootable}
	if source.Bootable && source.ImageMetadataReady {
		provenance.image = source.ImageSource
	}
	return s.createVolume(name, description, size, metadata, provenance)
}
