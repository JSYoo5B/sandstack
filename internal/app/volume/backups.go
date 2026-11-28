package volume

import (
	"cmp"
	"errors"
	"maps"
	"slices"
	"strings"

	domain "github.com/JSYoo5B/sandstack/internal/domain/volume"
)

var (
	ErrBackupNotFound           = errors.New("backup not found")
	ErrBackupStateConflict      = errors.New("backup state does not permit the operation")
	ErrBackupHasDependents      = errors.New("incremental backups exist for this backup")
	ErrNoIncrementalParent      = errors.New("no backups available to do an incremental backup")
	ErrBackupSnapshotMismatch   = errors.New("the snapshot does not belong to the backup volume")
	ErrBackupVolumeState        = errors.New("the volume to back up must be available, or in-use with force enabled")
	ErrRestoreVolumeState       = errors.New("the volume to restore to must be available")
	ErrRestoreVolumeTooSmall    = errors.New("the volume is too small to restore the backup")
	ErrBackupRestoreNotPending  = errors.New("the backup has no pending restore")
	ErrBackupCreationNotPending = errors.New("backup creation cannot change to the requested outcome")
)

const (
	backupFailReason          = "SandStack injected a backup failure."
	restoreVolumeNamePrefix   = "restore_backup_"
	restoreVolumeDescription  = "auto-created_from_restore_from_backup"
	restoredBackupMetadataKey = "src_backup_id"
)

type BackupCreateOptions struct {
	Name        *string
	Description *string
	Container   *string
	Incremental bool
	Force       bool
	SnapshotID  string
}

type BackupFilter struct {
	Name     *string
	Status   *domain.State
	VolumeID *string
}

func (f BackupFilter) matches(backup domain.Backup) bool {
	return ListFilter{Name: f.Name, Status: f.Status}.matches(backup.Name, backup.State) &&
		(f.VolumeID == nil || backup.VolumeID == *f.VolumeID)
}

type BackupPage struct {
	Backups    []domain.Backup
	NextMarker string
}

func optionalText(value *string) domain.NullableText {
	if value == nil {
		return domain.NullableText{}
	}
	return domain.NullableText{Value: *value, Valid: true}
}

func (s *Service) CreateBackup(projectID, volumeID string, options BackupCreateOptions) (domain.Backup, error) {
	if s.seed.ProjectID != projectID {
		return domain.Backup{}, ErrVolumeNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	volume, ok := s.volumes[volumeID]
	if !ok {
		return domain.Backup{}, ErrVolumeNotFound
	}
	var snapshot domain.Snapshot
	if options.SnapshotID != "" {
		snapshot, ok = s.snapshots[options.SnapshotID]
		switch {
		case !ok:
			return domain.Backup{}, ErrSnapshotNotFound
		case snapshot.VolumeID != volumeID:
			return domain.Backup{}, ErrBackupSnapshotMismatch
		case snapshot.State != domain.StateAvailable:
			return domain.Backup{}, ErrSnapshotStateConflict
		}
	} else if volume.State != domain.StateAvailable && !(options.Force && volume.State == domain.StateInUse) {
		return domain.Backup{}, ErrBackupVolumeState
	}
	var parentID string
	if options.Incremental {
		parent, found := s.incrementalParent(volumeID, snapshot)
		if !found {
			return domain.Backup{}, ErrNoIncrementalParent
		}
		parentID = parent.ID
	}
	id, err := s.allocateID()
	if err != nil {
		return domain.Backup{}, err
	}
	now := s.now().UTC()
	backup := domain.Backup{
		ID:          id,
		ProjectID:   projectID,
		VolumeID:    volumeID,
		SnapshotID:  options.SnapshotID,
		ParentID:    parentID,
		Name:        optionalText(options.Name),
		Description: optionalText(options.Description),
		Container:   optionalText(options.Container),
		Size:        volume.Size,
		State:       domain.StateCreating,
		CreatedAt:   now,
	}
	if options.SnapshotID != "" {
		backup.DataTimestamp = snapshot.CreatedAt
		snapshot.State = domain.StateBackingUp
		snapshot.UpdatedAt = now
		s.snapshots[snapshot.ID] = snapshot
	} else {
		backup.DataTimestamp = now
		backup.UpdatedAt = now
		volume.PreviousState = volume.State
		s.updateState(volume, domain.StateBackingUp)
	}
	s.backups[id] = backup
	return cloneBackup(backup), nil
}

// incrementalParent runs with the service's write lock held.
func (s *Service) incrementalParent(volumeID string, snapshot domain.Snapshot) (domain.Backup, bool) {
	var candidates []domain.Backup
	for _, backup := range s.backups {
		if backup.VolumeID != volumeID ||
			(backup.State != domain.StateAvailable && backup.State != domain.StateRestoring) ||
			(snapshot.ID != "" && !backup.DataTimestamp.Before(snapshot.CreatedAt)) {
			continue
		}
		candidates = append(candidates, backup)
	}
	if len(candidates) == 0 {
		return domain.Backup{}, false
	}
	return slices.MaxFunc(candidates, func(a, b domain.Backup) int {
		return cmp.Or(
			a.DataTimestamp.Compare(b.DataTimestamp),
			a.CreatedAt.Compare(b.CreatedAt),
			strings.Compare(a.ID, b.ID),
		)
	}), true
}

func (s *Service) GetBackup(projectID, backupID string) (domain.Backup, error) {
	if s.seed.ProjectID != projectID {
		return domain.Backup{}, ErrBackupNotFound
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	backup, ok := s.backups[backupID]
	if !ok {
		return domain.Backup{}, ErrBackupNotFound
	}
	return cloneBackup(backup), nil
}

// ListBackupsPage returns matching backups after the marker in ID order.
// A negative limit is unbounded and a full positive page exposes its last ID.
func (s *Service) ListBackupsPage(
	projectID string,
	filter BackupFilter,
	marker *string,
	limit int,
) (BackupPage, error) {
	if s.seed.ProjectID != projectID {
		if marker != nil {
			return BackupPage{}, ErrBackupNotFound
		}
		return BackupPage{Backups: []domain.Backup{}}, nil
	}
	s.mu.RLock()
	var after string
	if marker != nil {
		after = *marker
		if _, ok := s.backups[after]; !ok || after == "" {
			s.mu.RUnlock()
			return BackupPage{}, ErrBackupNotFound
		}
	}
	if limit == 0 {
		s.mu.RUnlock()
		return BackupPage{Backups: []domain.Backup{}}, nil
	}
	backups := make([]domain.Backup, 0, len(s.backups))
	for _, backup := range s.backups {
		if (marker == nil || backup.ID > after) && filter.matches(backup) {
			backups = append(backups, backup)
		}
	}
	s.mu.RUnlock()
	backups, next := orderAndLimit(backups, limit, func(backup domain.Backup) string { return backup.ID })
	for i, backup := range backups {
		backups[i] = cloneBackup(backup)
	}
	return BackupPage{Backups: backups, NextMarker: next}, nil
}

func (s *Service) DeleteBackup(projectID, backupID string) error {
	if s.seed.ProjectID != projectID {
		return ErrBackupNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	backup, ok := s.backups[backupID]
	if !ok {
		return ErrBackupNotFound
	}
	if backup.State != domain.StateAvailable && backup.State != domain.StateError {
		return ErrBackupStateConflict
	}
	for _, other := range s.backups {
		if other.ParentID == backupID {
			return ErrBackupHasDependents
		}
	}
	if parent, found := s.backups[backup.ParentID]; found && parent.DependentBackups > 0 {
		parent.DependentBackups--
		s.backups[parent.ID] = parent
	}
	delete(s.backups, backupID)
	return nil
}

// FinishBackupCreation applies a terminal outcome once and releases the source.
func (s *Service) FinishBackupCreation(backupID string, failed bool) error {
	state := domain.StateAvailable
	if failed {
		state = domain.StateError
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	backup, ok := s.backups[backupID]
	if !ok {
		return ErrBackupNotFound
	}
	if backup.State == state {
		return nil
	}
	if backup.State != domain.StateCreating {
		return ErrBackupCreationNotPending
	}
	now := s.now().UTC()
	volume := s.volumes[backup.VolumeID]
	if snapshot, found := s.snapshots[backup.SnapshotID]; found {
		snapshot.State = domain.StateAvailable
		snapshot.UpdatedAt = now
		s.snapshots[snapshot.ID] = snapshot
	} else if volume.State == domain.StateBackingUp {
		previous := volume.PreviousState
		volume.PreviousState = domain.StateBackingUp
		s.updateState(volume, previous)
	}
	if failed {
		backup.FailReason = backupFailReason
	} else {
		backup.Size = volume.Size
		backup.Source = domain.BackupSource{
			Name:               volume.Name,
			Description:        volume.Description,
			Metadata:           maps.Clone(volume.Metadata),
			ImageSource:        volume.ImageSource,
			ImageMetadataReady: volume.ImageMetadataReady,
		}
		if parent, found := s.backups[backup.ParentID]; found {
			parent.DependentBackups++
			s.backups[parent.ID] = parent
		}
	}
	backup.State = state
	backup.UpdatedAt = now
	s.backups[backupID] = backup
	return nil
}

// RestoreBackup restores into an available volume, or into a new volume when volumeID is empty.
func (s *Service) RestoreBackup(projectID, backupID, volumeID string, name *string) (domain.Restore, error) {
	if s.seed.ProjectID != projectID {
		return domain.Restore{}, ErrBackupNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	backup, ok := s.backups[backupID]
	if !ok {
		return domain.Restore{}, ErrBackupNotFound
	}
	if backup.State != domain.StateAvailable {
		return domain.Restore{}, ErrBackupStateConflict
	}
	var volume domain.Volume
	if volumeID == "" {
		volumeName := restoreVolumeNamePrefix + backupID
		if name != nil {
			volumeName = *name
		}
		id, err := s.allocateID()
		if err != nil {
			return domain.Restore{}, err
		}
		volume = domain.Volume{
			ID:          id,
			Name:        domain.NullableText{Value: volumeName, Valid: true},
			Description: domain.NullableText{Value: restoreVolumeDescription, Valid: true},
			ProjectID:   s.seed.ProjectID,
			UserID:      s.seed.UserID,
			Size:        backup.Size,
			CreatedAt:   s.now().UTC(),
			State:       domain.StateAvailable,
		}
	} else {
		volume, ok = s.volumes[volumeID]
		if !ok {
			return domain.Restore{}, ErrVolumeNotFound
		}
	}
	if volume.State != domain.StateAvailable {
		return domain.Restore{}, ErrRestoreVolumeState
	}
	if backup.Size > volume.Size {
		return domain.Restore{}, ErrRestoreVolumeTooSmall
	}
	backup.State = domain.StateRestoring
	backup.RestoreVolumeID = volume.ID
	backup.UpdatedAt = s.now().UTC()
	s.backups[backupID] = backup
	s.updateState(volume, domain.StateRestoringBackup)
	return domain.Restore{BackupID: backupID, VolumeID: volume.ID, VolumeName: volume.Name}, nil
}

// FinishBackupRestore applies the pending restore outcome to the target volume.
func (s *Service) FinishBackupRestore(backupID string, failed bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	backup, ok := s.backups[backupID]
	if !ok {
		return ErrBackupNotFound
	}
	if backup.State != domain.StateRestoring {
		return ErrBackupRestoreNotPending
	}
	volume, found := s.volumes[backup.RestoreVolumeID]
	if found && volume.State == domain.StateRestoringBackup {
		if failed {
			s.updateState(volume, domain.StateErrorRestoring)
		} else {
			s.updateState(restoreSource(volume, backup), domain.StateAvailable)
		}
	}
	backup.State = domain.StateAvailable
	backup.RestoreVolumeID = ""
	backup.UpdatedAt = s.now().UTC()
	s.backups[backupID] = backup
	return nil
}

func restoreSource(volume domain.Volume, backup domain.Backup) domain.Volume {
	source := backup.Source
	if source.Name.Valid && source.Name.Value != "" &&
		(volume.Name.Value == "" || strings.HasPrefix(volume.Name.Value, restoreVolumeNamePrefix)) {
		volume.Name = source.Name
		volume.Description = source.Description
	}
	volume.Metadata = maps.Clone(volume.Metadata)
	if len(source.Metadata) > 0 {
		volume.Metadata = maps.Clone(source.Metadata)
	}
	if source.ImageMetadataReady && source.ImageSource.ID != "" {
		volume.ImageSource = source.ImageSource
		volume.ImageMetadataReady = true
		volume.Bootable = true
	}
	if backup.VolumeID != volume.ID || (volume.Metadata[restoredBackupMetadataKey] != "" &&
		volume.Metadata[restoredBackupMetadataKey] != backup.ID) {
		if volume.Metadata == nil {
			volume.Metadata = map[string]string{}
		}
		volume.Metadata[restoredBackupMetadataKey] = backup.ID
	}
	return volume
}

func cloneBackup(backup domain.Backup) domain.Backup {
	backup.Source.Metadata = maps.Clone(backup.Source.Metadata)
	return backup
}
