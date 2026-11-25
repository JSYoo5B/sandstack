package volume

import (
	"errors"

	domain "github.com/JSYoo5B/sandstack/internal/domain/volume"
)

var ErrVolumeSmallerThanSnapshot = errors.New("volume size must be at least the snapshot size")

func (s *Service) Create(projectID, name, description string, size int) (domain.Volume, error) {
	if s.seed.ProjectID != projectID {
		return domain.Volume{}, ErrVolumeNotFound
	}
	if size <= 0 {
		return domain.Volume{}, ErrInvalidVolumeSize
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.createVolume(name, description, size, "")
}

// CreateFromSnapshot uses the snapshot's size when size is omitted (zero).
func (s *Service) CreateFromSnapshot(
	projectID, snapshotID, name, description string,
	size int,
) (domain.Volume, error) {
	if s.seed.ProjectID != projectID {
		return domain.Volume{}, ErrSnapshotNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	snapshot, ok := s.snapshots[snapshotID]
	if !ok {
		return domain.Volume{}, ErrSnapshotNotFound
	}
	if snapshot.State != domain.StateAvailable {
		return domain.Volume{}, ErrSnapshotStateConflict
	}
	if size == 0 {
		size = snapshot.Size
	}
	if size <= 0 {
		return domain.Volume{}, ErrInvalidVolumeSize
	}
	if size < snapshot.Size {
		return domain.Volume{}, ErrVolumeSmallerThanSnapshot
	}
	return s.createVolume(name, description, size, snapshotID)
}

// createVolume runs with the service's write lock held.
func (s *Service) createVolume(name, description string, size int, snapshotID string) (domain.Volume, error) {
	id, err := s.allocateID()
	if err != nil {
		return domain.Volume{}, err
	}
	volume := domain.Volume{
		ID:          id,
		Name:        name,
		Description: description,
		ProjectID:   s.seed.ProjectID,
		UserID:      s.seed.UserID,
		SnapshotID:  snapshotID,
		Size:        size,
		CreatedAt:   s.now().UTC(),
		State:       domain.StateCreating,
	}
	s.volumes[id] = volume
	return volume, nil
}
