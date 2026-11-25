package volume

import (
	"errors"
	"maps"
	"time"

	domain "github.com/JSYoo5B/sandstack/internal/domain/volume"
)

var (
	ErrSnapshotNotFound      = errors.New("snapshot not found")
	ErrSnapshotStateConflict = errors.New("snapshot state does not permit the operation")
	ErrVolumeHasSnapshots    = errors.New("volume has snapshots")
)

type SnapshotCreateOptions struct {
	Name        string
	Description string
	Force       bool
	Metadata    map[string]string
}

func (s *Service) CreateSnapshot(
	projectID, volumeID string,
	options SnapshotCreateOptions,
) (domain.Snapshot, error) {
	if s.seed.ProjectID != projectID {
		return domain.Snapshot{}, ErrVolumeNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	volume, ok := s.volumes[volumeID]
	if !ok {
		return domain.Snapshot{}, ErrVolumeNotFound
	}
	if volume.State != domain.StateAvailable && !(options.Force && volume.State == domain.StateInUse) {
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
		Name:        domain.NullableText{Value: options.Name, Valid: options.Name != ""},
		Description: domain.NullableText{Value: options.Description, Valid: options.Description != ""},
		Size:        volume.Size,
		CreatedAt:   s.now().UTC(),
		State:       domain.StateCreating,
		Metadata:    copySnapshotMetadata(options.Metadata),
	}
	s.snapshots[id] = snapshot
	return cloneSnapshot(snapshot), nil
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
	return cloneSnapshot(snapshot), nil
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
	return cloneSnapshot(snapshot), before.UpdatedAt, nil
}

// ReplaceSnapshotMetadata changes only the metadata on a scoped snapshot.
func (s *Service) ReplaceSnapshotMetadata(projectID, snapshotID string, metadata map[string]string) (map[string]string, error) {
	return s.writeSnapshotMetadata(projectID, snapshotID, metadata, true)
}

// MergeSnapshotMetadata sets supplied keys and preserves the other metadata.
func (s *Service) MergeSnapshotMetadata(projectID, snapshotID string, metadata map[string]string) (map[string]string, error) {
	return s.writeSnapshotMetadata(projectID, snapshotID, metadata, false)
}

func (s *Service) writeSnapshotMetadata(
	projectID, snapshotID string,
	metadata map[string]string,
	replace bool,
) (map[string]string, error) {
	if s.seed.ProjectID != projectID {
		return nil, ErrSnapshotNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	snapshot, ok := s.snapshots[snapshotID]
	if !ok {
		return nil, ErrSnapshotNotFound
	}
	// Stored metadata maps are immutable, including maps observed by list readers.
	switch {
	case replace:
		snapshot.Metadata = copySnapshotMetadata(metadata)
	case len(metadata) == 0:
		return maps.Clone(snapshot.Metadata), nil
	default:
		snapshot.Metadata = copySnapshotMetadata(snapshot.Metadata)
		maps.Copy(snapshot.Metadata, metadata)
	}
	s.snapshots[snapshotID] = snapshot
	return maps.Clone(snapshot.Metadata), nil
}

func copySnapshotMetadata(metadata map[string]string) map[string]string {
	copied := make(map[string]string, len(metadata))
	maps.Copy(copied, metadata)
	return copied
}

func cloneSnapshot(snapshot domain.Snapshot) domain.Snapshot {
	snapshot.Metadata = maps.Clone(snapshot.Metadata)
	return snapshot
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
	for i, snapshot := range snapshots {
		snapshots[i] = cloneSnapshot(snapshot)
	}
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
