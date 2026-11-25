package volume

import (
	"errors"
	"slices"
	"strings"

	domain "github.com/JSYoo5B/sandstack/internal/domain/volume"
)

var (
	ErrSnapshotNotFound      = errors.New("snapshot not found")
	ErrSnapshotStateConflict = errors.New("snapshot state does not permit the operation")
	ErrVolumeHasSnapshots    = errors.New("volume has snapshots")
)

func (s *Service) CreateSnapshot(projectID, volumeID, name, description string) (domain.Snapshot, error) {
	if s.seed.ProjectID != projectID {
		return domain.Snapshot{}, ErrVolumeNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	volume, ok := s.volumes[volumeID]
	if !ok {
		return domain.Snapshot{}, ErrVolumeNotFound
	}
	if volume.State != domain.StateAvailable {
		return domain.Snapshot{}, ErrVolumeStateConflict
	}
	id, err := s.allocateID()
	if err != nil {
		return domain.Snapshot{}, err
	}
	snapshot := domain.Snapshot{
		ID:          id,
		ProjectID:   projectID,
		VolumeID:    volumeID,
		Name:        name,
		Description: description,
		Size:        volume.Size,
		CreatedAt:   s.now().UTC(),
		State:       domain.StateCreating,
	}
	s.snapshots[id] = snapshot
	return snapshot, nil
}

func (s *Service) GetSnapshot(projectID, snapshotID string) (domain.Snapshot, error) {
	if s.seed.ProjectID != projectID {
		return domain.Snapshot{}, ErrSnapshotNotFound
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	snapshot, ok := s.snapshots[snapshotID]
	if !ok {
		return domain.Snapshot{}, ErrSnapshotNotFound
	}
	return snapshot, nil
}

// ListSnapshots returns the project's snapshots ordered by ID.
func (s *Service) ListSnapshots(projectID string) []domain.Snapshot {
	if s.seed.ProjectID != projectID {
		return []domain.Snapshot{}
	}
	s.mu.RLock()
	snapshots := make([]domain.Snapshot, 0, len(s.snapshots))
	for _, snapshot := range s.snapshots {
		snapshots = append(snapshots, snapshot)
	}
	s.mu.RUnlock()
	slices.SortFunc(snapshots, func(a, b domain.Snapshot) int { return strings.Compare(a.ID, b.ID) })
	return snapshots
}

func (s *Service) DeleteSnapshot(projectID, snapshotID string) error {
	if s.seed.ProjectID != projectID {
		return ErrSnapshotNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot, ok := s.snapshots[snapshotID]
	if !ok {
		return ErrSnapshotNotFound
	}
	if snapshot.State != domain.StateAvailable && snapshot.State != domain.StateError {
		return ErrSnapshotStateConflict
	}
	delete(s.snapshots, snapshotID)
	return nil
}

// FinishSnapshotCreation applies a terminal outcome once.
func (s *Service) FinishSnapshotCreation(snapshotID string, failed bool) error {
	state := domain.StateAvailable
	if failed {
		state = domain.StateError
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot, ok := s.snapshots[snapshotID]
	if !ok {
		return ErrSnapshotNotFound
	}
	if snapshot.State == state {
		return nil
	}
	if snapshot.State != domain.StateCreating {
		return ErrSnapshotStateConflict
	}
	snapshot.UpdatedAt = s.now().UTC()
	snapshot.State = state
	s.snapshots[snapshotID] = snapshot
	return nil
}
