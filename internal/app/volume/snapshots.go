package volume

import (
	"errors"
	"time"

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
		Name:        domain.NullableText{Value: name, Valid: name != ""},
		Description: domain.NullableText{Value: description, Valid: description != ""},
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

func (s *Service) UpdateSnapshot(
	projectID, snapshotID string,
	changes TextUpdate,
) (domain.Snapshot, time.Time, error) {
	if s.seed.ProjectID != projectID {
		return domain.Snapshot{}, time.Time{}, ErrSnapshotNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	snapshot, ok := s.snapshots[snapshotID]
	if !ok {
		return domain.Snapshot{}, time.Time{}, ErrSnapshotNotFound
	}
	before := snapshot
	if changes.Name != nil {
		snapshot.Name = *changes.Name
	}
	if changes.Description != nil {
		snapshot.Description = *changes.Description
	}
	if snapshot.Name != before.Name || snapshot.Description != before.Description {
		snapshot.UpdatedAt = s.now().UTC()
		s.snapshots[snapshotID] = snapshot
	}
	return snapshot, before.UpdatedAt, nil
}

// ListSnapshots returns the project's snapshots ordered by ID.
func (s *Service) ListSnapshots(projectID string) []domain.Snapshot {
	page, _ := s.ListSnapshotsPage(projectID, ListFilter{}, nil, -1)
	return page.Snapshots
}

type SnapshotPage struct {
	Snapshots  []domain.Snapshot
	NextMarker string
}

// ListSnapshotsPage returns matching snapshots after the marker in ID order.
// A negative limit is unbounded and a full positive page exposes its last ID.
func (s *Service) ListSnapshotsPage(
	projectID string,
	filter ListFilter,
	marker *string,
	limit int,
) (SnapshotPage, error) {
	if s.seed.ProjectID != projectID {
		if marker != nil {
			return SnapshotPage{}, ErrSnapshotNotFound
		}
		return SnapshotPage{Snapshots: []domain.Snapshot{}}, nil
	}
	s.mu.RLock()
	var after string
	if marker != nil {
		after = *marker
		if _, ok := s.snapshots[after]; !ok || after == "" {
			s.mu.RUnlock()
			return SnapshotPage{}, ErrSnapshotNotFound
		}
	}
	if limit == 0 {
		s.mu.RUnlock()
		return SnapshotPage{Snapshots: []domain.Snapshot{}}, nil
	}
	snapshots := make([]domain.Snapshot, 0, len(s.snapshots))
	for _, snapshot := range s.snapshots {
		if (marker == nil || snapshot.ID > after) && filter.matches(snapshot.Name, snapshot.State) {
			snapshots = append(snapshots, snapshot)
		}
	}
	s.mu.RUnlock()
	snapshots, next := orderAndLimit(snapshots, limit, func(snapshot domain.Snapshot) string { return snapshot.ID })
	return SnapshotPage{Snapshots: snapshots, NextMarker: next}, nil
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
