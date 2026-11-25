package volume

import (
	"errors"
	"maps"

	domain "github.com/JSYoo5B/sandstack/internal/domain/volume"
)

var ErrVolumeSmallerThanSnapshot = errors.New("volume size must be at least the snapshot size")

func (s *Service) Create(projectID, name, description string, size int) (domain.Volume, error) {
	return s.CreateWithMetadata(projectID, name, description, size, nil)
}

func (s *Service) CreateWithMetadata(
	projectID, name, description string,
	size int,
	metadata map[string]string,
) (domain.Volume, error) {
	if s.seed.ProjectID != projectID {
		return domain.Volume{}, ErrVolumeNotFound
	}
	if size <= 0 {
		return domain.Volume{}, ErrInvalidVolumeSize
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.createVolume(name, description, size, metadata, volumeSource{})
}

// CreateFromSnapshot uses the snapshot's size when size is omitted (zero).
func (s *Service) CreateFromSnapshot(
	projectID, snapshotID, name, description string,
	size int,
) (domain.Volume, error) {
	return s.CreateFromSnapshotWithMetadata(projectID, snapshotID, name, description, size, nil)
}

// CreateFromSnapshotWithMetadata assigns the requested metadata to the new volume.
func (s *Service) CreateFromSnapshotWithMetadata(
	projectID, snapshotID, name, description string,
	size int,
	metadata map[string]string,
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
	return s.createVolume(name, description, size, metadata, volumeSource{snapshotID: snapshotID})
}

type volumeSource struct {
	snapshotID string
	volumeID   string
	image      domain.ImageSource
	bootable   bool
}

// createVolume runs with the service's write lock held.
func (s *Service) createVolume(
	name, description string,
	size int,
	metadata map[string]string,
	source volumeSource,
) (domain.Volume, error) {
	id, err := s.allocateID()
	if err != nil {
		return domain.Volume{}, err
	}
	volume := domain.Volume{
		ID:             id,
		Name:           domain.NullableText{Value: name, Valid: name != ""},
		Description:    domain.NullableText{Value: description, Valid: description != ""},
		ProjectID:      s.seed.ProjectID,
		UserID:         s.seed.UserID,
		SnapshotID:     source.snapshotID,
		SourceVolumeID: source.volumeID,
		Size:           size,
		CreatedAt:      s.now().UTC(),
		State:          domain.StateCreating,
		Metadata:       maps.Clone(metadata),
		ImageSource:    source.image,
		Bootable:       source.bootable,
	}
	s.volumes[id] = volume
	return cloneVolume(volume), nil
}
