package volume_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	app "github.com/JSYoo5B/sandstack/internal/app/volume"
	domain "github.com/JSYoo5B/sandstack/internal/domain/volume"
)

type backupFixture struct {
	service *app.Service
	seed    domain.Volume
	now     time.Time
}

func newBackupFixture(seed domain.Volume) *backupFixture {
	fixture := &backupFixture{seed: seed, now: time.Date(2026, time.November, 28, 3, 0, 0, 0, time.UTC)}
	id := 0x100
	fixture.service = app.New(seed, func() time.Time { return fixture.now }, func() string {
		id++
		return fmt.Sprintf("%08x-0000-4000-8000-000000000000", id)
	})
	return fixture
}

func (f *backupFixture) tick() {
	f.now = f.now.Add(time.Minute)
}

func (f *backupFixture) volume(t *testing.T, id string) domain.Volume {
	t.Helper()
	volume, err := f.service.Get(f.seed.ProjectID, id)
	require.NoError(t, err)
	return volume
}

func (f *backupFixture) backup(t *testing.T, id string) domain.Backup {
	t.Helper()
	backup, err := f.service.GetBackup(f.seed.ProjectID, id)
	require.NoError(t, err)
	return backup
}

func text(value string) *string {
	return &value
}

// https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/backup/api.py#L83-L118
// https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/backup/api.py#L218-L369
// https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/backup/manager.py#L585-L620
// https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/db/api.py#L6543-L6578
func TestBackupCreationTracksVolumeStatusAndIncrementalChain(t *testing.T) {
	fixture := newBackupFixture(domain.Volume{ID: fixtureID, ProjectID: "project", Size: 2, State: domain.StateAvailable})
	service, seed := fixture.service, fixture.seed
	start := fixture.now

	_, foreignErr := service.CreateBackup("foreign", seed.ID, app.BackupCreateOptions{})
	_, missingErr := service.CreateBackup(seed.ProjectID, "missing", app.BackupCreateOptions{})
	_, noParentErr := service.CreateBackup(seed.ProjectID, seed.ID, app.BackupCreateOptions{Incremental: true})

	assert.ErrorIs(t, foreignErr, app.ErrVolumeNotFound)
	assert.ErrorIs(t, missingErr, app.ErrVolumeNotFound)
	assert.ErrorIs(t, noParentErr, app.ErrNoIncrementalParent)
	assert.Equal(t, seed, fixture.volume(t, seed.ID))

	full, err := service.CreateBackup(seed.ProjectID, seed.ID, app.BackupCreateOptions{
		Name: text("full"), Description: text(""),
	})
	require.NoError(t, err)
	backingUp := fixture.volume(t, seed.ID)
	_, busyErr := service.CreateBackup(seed.ProjectID, seed.ID, app.BackupCreateOptions{Force: true})
	_, pendingParentErr := service.CreateBackup(seed.ProjectID, seed.ID, app.BackupCreateOptions{Incremental: true})

	assert.Equal(t, domain.Backup{
		ID:            full.ID,
		ProjectID:     seed.ProjectID,
		VolumeID:      seed.ID,
		Name:          domain.NullableText{Value: "full", Valid: true},
		Description:   domain.NullableText{Valid: true},
		Size:          seed.Size,
		State:         domain.StateCreating,
		CreatedAt:     start,
		UpdatedAt:     start,
		DataTimestamp: start,
	}, full)
	assert.Equal(t, domain.StateBackingUp, backingUp.State)
	assert.Equal(t, domain.StateAvailable, backingUp.PreviousState)
	assert.ErrorIs(t, busyErr, app.ErrBackupVolumeState)
	assert.ErrorIs(t, pendingParentErr, app.ErrBackupVolumeState)
	assert.ErrorIs(t, service.Delete(seed.ProjectID, seed.ID), app.ErrVolumeStateConflict)
	assert.ErrorIs(t, service.DeleteBackup(seed.ProjectID, full.ID), app.ErrBackupStateConflict)

	fixture.tick()
	require.NoError(t, service.FinishBackupCreation(full.ID, false))
	require.NoError(t, service.FinishBackupCreation(full.ID, false))
	repeatErr := service.FinishBackupCreation(full.ID, true)
	released := fixture.volume(t, seed.ID)
	fixture.tick()
	incremental, err := service.CreateBackup(seed.ProjectID, seed.ID, app.BackupCreateOptions{Incremental: true})
	require.NoError(t, err)
	parentWhilePending := fixture.backup(t, full.ID)
	require.NoError(t, service.FinishBackupCreation(incremental.ID, false))
	parent := fixture.backup(t, full.ID)

	assert.ErrorIs(t, repeatErr, app.ErrBackupCreationNotPending)
	assert.Equal(t, domain.StateAvailable, released.State)
	assert.Equal(t, domain.StateBackingUp, released.PreviousState)
	assert.Equal(t, start.Add(time.Minute), released.UpdatedAt)
	assert.Equal(t, full.ID, incremental.ParentID)
	assert.Zero(t, parentWhilePending.DependentBackups)
	assert.Equal(t, 1, parent.DependentBackups)
	assert.Equal(t, domain.StateAvailable, parent.State)

	assert.ErrorIs(t, service.DeleteBackup(seed.ProjectID, full.ID), app.ErrBackupHasDependents)
	assert.ErrorIs(t, service.DeleteBackup("foreign", incremental.ID), app.ErrBackupNotFound)
	require.NoError(t, service.DeleteBackup(seed.ProjectID, incremental.ID))
	assert.Zero(t, fixture.backup(t, full.ID).DependentBackups)
	require.NoError(t, service.Delete(seed.ProjectID, seed.ID))
	orphan := fixture.backup(t, full.ID)

	assert.Equal(t, seed.ID, orphan.VolumeID)
	require.NoError(t, service.DeleteBackup(seed.ProjectID, full.ID))
	_, err = service.GetBackup(seed.ProjectID, full.ID)
	assert.ErrorIs(t, err, app.ErrBackupNotFound)
}

// https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/backup/api.py#L230-L255
// https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/backup/manager.py#L557-L568
func TestForcedBackupOfAttachedVolumeRestoresInUse(t *testing.T) {
	seed := domain.Volume{
		ID: fixtureID, ProjectID: "project", Size: 1, State: domain.StateInUse,
		Attachment: domain.Attachment{ID: "attachment", VolumeID: fixtureID, ServerID: "server"},
	}
	fixture := newBackupFixture(seed)
	service := fixture.service

	_, unforcedErr := service.CreateBackup(seed.ProjectID, seed.ID, app.BackupCreateOptions{})
	backup, err := service.CreateBackup(seed.ProjectID, seed.ID, app.BackupCreateOptions{Force: true})
	require.NoError(t, err)
	detachErr := service.Detach(seed.ProjectID, "server", seed.ID)
	faultErr := service.SetFailed(seed.ID, true)
	require.NoError(t, service.FinishBackupCreation(backup.ID, true))
	failed := fixture.backup(t, backup.ID)
	attached := fixture.volume(t, seed.ID)

	assert.ErrorIs(t, unforcedErr, app.ErrBackupVolumeState)
	assert.ErrorIs(t, detachErr, app.ErrVolumeStateConflict)
	assert.ErrorIs(t, faultErr, app.ErrVolumeStateConflict)
	assert.Equal(t, domain.StateError, failed.State)
	assert.NotEmpty(t, failed.FailReason)
	assert.Equal(t, domain.StateInUse, attached.State)
	assert.Equal(t, seed.Attachment, attached.Attachment)
	_, restoreErr := service.RestoreBackup(seed.ProjectID, seed.UserID, backup.ID, "", nil)
	assert.ErrorIs(t, restoreErr, app.ErrBackupStateConflict)
	require.NoError(t, service.DeleteBackup(seed.ProjectID, backup.ID))
}

// https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/backup/api.py#L230-L250
// https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/backup/api.py#L269-L328
func TestSnapshotBackupKeepsVolumeStatusAndSelectsEarlierParent(t *testing.T) {
	fixture := newBackupFixture(domain.Volume{ID: fixtureID, ProjectID: "project", Size: 1, State: domain.StateAvailable})
	service, seed := fixture.service, fixture.seed
	other, err := service.Create(seed.ProjectID, seed.UserID, "", "", 1)
	require.NoError(t, err)
	require.NoError(t, service.CompleteCreation(other.ID))
	early, err := service.CreateBackup(seed.ProjectID, seed.ID, app.BackupCreateOptions{})
	require.NoError(t, err)
	require.NoError(t, service.FinishBackupCreation(early.ID, false))
	fixture.tick()
	snapshot, err := service.CreateSnapshot(seed.ProjectID, seed.ID, app.SnapshotCreateOptions{})
	require.NoError(t, err)
	_, pendingErr := service.CreateBackup(seed.ProjectID, seed.ID, app.BackupCreateOptions{SnapshotID: snapshot.ID})
	require.NoError(t, service.FinishSnapshotCreation(snapshot.ID, false))
	fixture.tick()
	late, err := service.CreateBackup(seed.ProjectID, seed.ID, app.BackupCreateOptions{})
	require.NoError(t, err)
	require.NoError(t, service.FinishBackupCreation(late.ID, false))

	_, mismatchErr := service.CreateBackup(seed.ProjectID, other.ID, app.BackupCreateOptions{SnapshotID: snapshot.ID})
	_, missingErr := service.CreateBackup(seed.ProjectID, seed.ID, app.BackupCreateOptions{SnapshotID: other.ID})
	fromSnapshot, err := service.CreateBackup(seed.ProjectID, seed.ID, app.BackupCreateOptions{
		SnapshotID: snapshot.ID, Incremental: true,
	})
	require.NoError(t, err)
	busySnapshot, err := service.GetSnapshot(seed.ProjectID, snapshot.ID)
	require.NoError(t, err)

	assert.ErrorIs(t, pendingErr, app.ErrSnapshotStateConflict)
	assert.ErrorIs(t, mismatchErr, app.ErrBackupSnapshotMismatch)
	assert.ErrorIs(t, missingErr, app.ErrSnapshotNotFound)
	assert.Equal(t, early.ID, fromSnapshot.ParentID)
	assert.Equal(t, snapshot.CreatedAt, fromSnapshot.DataTimestamp)
	assert.True(t, fromSnapshot.UpdatedAt.IsZero())
	assert.Equal(t, domain.StateBackingUp, busySnapshot.State)
	assert.Equal(t, domain.StateAvailable, fixture.volume(t, seed.ID).State)
	assert.ErrorIs(t, service.DeleteSnapshot(seed.ProjectID, snapshot.ID), app.ErrSnapshotStateConflict)

	require.NoError(t, service.FinishBackupCreation(fromSnapshot.ID, false))
	released, err := service.GetSnapshot(seed.ProjectID, snapshot.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.StateAvailable, released.State)
	assert.Equal(t, 1, fixture.backup(t, early.ID).DependentBackups)
	assert.Zero(t, fixture.backup(t, late.ID).DependentBackups)
	latest, err := service.CreateBackup(seed.ProjectID, seed.ID, app.BackupCreateOptions{Incremental: true})
	require.NoError(t, err)
	assert.Equal(t, late.ID, latest.ParentID)
}

// https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/backup/api.py#L371-L451
// https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/backup/manager.py#L645-L768
// https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/backup/driver.py#L175-L305
func TestRestoreBackupIntoNewAndExistingVolumes(t *testing.T) {
	image := domain.ImageSource{ID: "image", Name: "cirros"}
	seed := domain.Volume{
		ID: fixtureID, ProjectID: "project", UserID: "user", Size: 2, State: domain.StateAvailable,
		Name: domain.NullableText{Value: "source", Valid: true}, Description: domain.NullableText{Value: "origin", Valid: true},
		Metadata: map[string]string{"team": "storage"}, ImageSource: image, ImageMetadataReady: true, Bootable: true,
	}
	fixture := newBackupFixture(seed)
	service := fixture.service
	backup, err := service.CreateBackup(seed.ProjectID, seed.ID, app.BackupCreateOptions{})
	require.NoError(t, err)
	_, pendingErr := service.RestoreBackup(seed.ProjectID, seed.UserID, backup.ID, "", nil)
	require.NoError(t, service.FinishBackupCreation(backup.ID, false))
	small, err := service.Create(seed.ProjectID, seed.UserID, "small", "", 1)
	require.NoError(t, err)
	_, creatingErr := service.RestoreBackup(seed.ProjectID, seed.UserID, backup.ID, small.ID, nil)
	require.NoError(t, service.CompleteCreation(small.ID))

	_, foreignErr := service.RestoreBackup("foreign", "foreign-user", backup.ID, "", nil)
	_, missingBackupErr := service.RestoreBackup(seed.ProjectID, seed.UserID, "missing", "", nil)
	_, missingVolumeErr := service.RestoreBackup(seed.ProjectID, seed.UserID, backup.ID, "missing", nil)
	_, smallErr := service.RestoreBackup(seed.ProjectID, seed.UserID, backup.ID, small.ID, nil)
	fixture.tick()
	restore, err := service.RestoreBackup(seed.ProjectID, seed.UserID, backup.ID, "", nil)
	require.NoError(t, err)
	restoring := fixture.volume(t, restore.VolumeID)
	_, busyErr := service.RestoreBackup(seed.ProjectID, seed.UserID, backup.ID, seed.ID, nil)

	assert.ErrorIs(t, pendingErr, app.ErrBackupStateConflict)
	assert.ErrorIs(t, creatingErr, app.ErrRestoreVolumeState)
	assert.ErrorIs(t, foreignErr, app.ErrBackupNotFound)
	assert.ErrorIs(t, missingBackupErr, app.ErrBackupNotFound)
	assert.ErrorIs(t, missingVolumeErr, app.ErrVolumeNotFound)
	assert.ErrorIs(t, smallErr, app.ErrRestoreVolumeTooSmall)
	assert.ErrorIs(t, busyErr, app.ErrBackupStateConflict)
	assert.Equal(t, domain.Restore{
		BackupID:   backup.ID,
		VolumeID:   restoring.ID,
		VolumeName: domain.NullableText{Value: "restore_backup_" + backup.ID, Valid: true},
	}, restore)
	assert.Equal(t, domain.Volume{
		ID:          restore.VolumeID,
		Name:        restore.VolumeName,
		Description: domain.NullableText{Value: "auto-created_from_restore_from_backup", Valid: true},
		ProjectID:   seed.ProjectID,
		UserID:      seed.UserID,
		Size:        backup.Size,
		CreatedAt:   fixture.now,
		UpdatedAt:   fixture.now,
		State:       domain.StateRestoringBackup,
	}, restoring)
	assert.Equal(t, domain.StateRestoring, fixture.backup(t, backup.ID).State)
	assert.ErrorIs(t, service.DeleteBackup(seed.ProjectID, backup.ID), app.ErrBackupStateConflict)
	assert.ErrorIs(t, service.Delete(seed.ProjectID, restore.VolumeID), app.ErrVolumeStateConflict)

	require.NoError(t, service.FinishBackupRestore(backup.ID, false))
	repeatErr := service.FinishBackupRestore(backup.ID, false)
	restored := fixture.volume(t, restore.VolumeID)

	assert.ErrorIs(t, repeatErr, app.ErrBackupRestoreNotPending)
	assert.Equal(t, domain.StateAvailable, restored.State)
	assert.Equal(t, seed.Name, restored.Name)
	assert.Equal(t, seed.Description, restored.Description)
	assert.Equal(t, map[string]string{"team": "storage", "src_backup_id": backup.ID}, restored.Metadata)
	assert.Equal(t, image, restored.ImageSource)
	assert.True(t, restored.ImageMetadataReady)
	assert.True(t, restored.Bootable)
	assert.Equal(t, domain.StateAvailable, fixture.backup(t, backup.ID).State)

	_, err = service.ReplaceVolumeMetadata(seed.ProjectID, seed.ID, map[string]string{})
	require.NoError(t, err)
	named, err := service.RestoreBackup(seed.ProjectID, seed.UserID, backup.ID, "", text("named"))
	require.NoError(t, err)
	require.NoError(t, service.FinishBackupRestore(backup.ID, true))
	failed := fixture.volume(t, named.VolumeID)
	inPlace, err := service.RestoreBackup(seed.ProjectID, seed.UserID, backup.ID, seed.ID, text("ignored"))
	require.NoError(t, err)
	require.NoError(t, service.FinishBackupRestore(backup.ID, false))
	original := fixture.volume(t, seed.ID)

	assert.Equal(t, domain.NullableText{Value: "named", Valid: true}, named.VolumeName)
	assert.Equal(t, domain.StateErrorRestoring, failed.State)
	assert.Equal(t, domain.NullableText{Value: "named", Valid: true}, failed.Name)
	assert.Equal(t, seed.Name, inPlace.VolumeName)
	assert.Equal(t, map[string]string{"team": "storage"}, original.Metadata)
	require.NoError(t, service.Delete(seed.ProjectID, named.VolumeID))

	service.Reset()
	_, err = service.GetBackup(seed.ProjectID, backup.ID)
	assert.ErrorIs(t, err, app.ErrBackupNotFound)
	page, err := service.ListBackupsPage(seed.ProjectID, app.BackupFilter{}, nil, -1)
	require.NoError(t, err)
	assert.Empty(t, page.Backups)
}

func TestListBackupsFiltersAndPages(t *testing.T) {
	fixture := newBackupFixture(domain.Volume{ID: fixtureID, ProjectID: "project", Size: 1, State: domain.StateAvailable})
	service, seed := fixture.service, fixture.seed
	other, err := service.Create(seed.ProjectID, seed.UserID, "", "", 1)
	require.NoError(t, err)
	require.NoError(t, service.CompleteCreation(other.ID))
	first, err := service.CreateBackup(seed.ProjectID, seed.ID, app.BackupCreateOptions{Name: text("daily")})
	require.NoError(t, err)
	require.NoError(t, service.FinishBackupCreation(first.ID, false))
	second, err := service.CreateBackup(seed.ProjectID, other.ID, app.BackupCreateOptions{Name: text("daily")})
	require.NoError(t, err)
	daily, creating := "daily", domain.StateCreating

	byName, err := service.ListBackupsPage(seed.ProjectID, app.BackupFilter{Name: &daily}, nil, -1)
	require.NoError(t, err)
	byVolume, err := service.ListBackupsPage(seed.ProjectID, app.BackupFilter{VolumeID: &other.ID}, nil, -1)
	require.NoError(t, err)
	byStatus, err := service.ListBackupsPage(seed.ProjectID, app.BackupFilter{Status: &creating}, nil, -1)
	require.NoError(t, err)
	paged, err := service.ListBackupsPage(seed.ProjectID, app.BackupFilter{}, nil, 1)
	require.NoError(t, err)
	next, err := service.ListBackupsPage(seed.ProjectID, app.BackupFilter{}, &paged.NextMarker, 1)
	require.NoError(t, err)
	foreign, err := service.ListBackupsPage("foreign", app.BackupFilter{}, nil, -1)
	require.NoError(t, err)
	missing := "missing"
	_, markerErr := service.ListBackupsPage(seed.ProjectID, app.BackupFilter{}, &missing, 1)

	assert.Len(t, byName.Backups, 2)
	assert.Equal(t, []domain.Backup{second}, byVolume.Backups)
	assert.Equal(t, []domain.Backup{second}, byStatus.Backups)
	assert.Equal(t, first.ID, paged.Backups[0].ID)
	assert.Equal(t, first.ID, paged.NextMarker)
	assert.Equal(t, []domain.Backup{second}, next.Backups)
	assert.Empty(t, foreign.Backups)
	assert.ErrorIs(t, markerErr, app.ErrBackupNotFound)
}
