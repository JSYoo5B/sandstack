package volume_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	app "github.com/JSYoo5B/sandstack/internal/app/volume"
	domain "github.com/JSYoo5B/sandstack/internal/domain/volume"
)

const (
	receiverProject = "receiver"
	receiverUser    = "receiver-user"
)

func newOwnershipFixture() *backupFixture {
	return newTransferFixture(domain.Volume{
		ID: fixtureID, ProjectID: "project", UserID: "user", Size: 1, State: domain.StateAvailable,
	})
}

// https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/db/api.py#L331-L370
// https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/db/api.py#L2377-L2395
// https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/db/api.py#L3783-L3797
// https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/db/api.py#L6368-L6390
// https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/volume/api.py#L633-L686
func TestProjectsSeeOnlyTheirOwnResources(t *testing.T) {
	fixture := newOwnershipFixture()
	service, seed := fixture.service, fixture.seed
	owned, err := service.Create(receiverProject, receiverUser, "receiver volume", "", 1)
	require.NoError(t, err)
	require.NoError(t, service.CompleteCreation(owned.ID))
	snapshot, err := service.CreateSnapshot(receiverProject, owned.ID, app.SnapshotCreateOptions{})
	require.NoError(t, err)
	require.NoError(t, service.FinishSnapshotCreation(snapshot.ID, false))
	backup, err := service.CreateBackup(receiverProject, owned.ID, app.BackupCreateOptions{})
	require.NoError(t, err)
	require.NoError(t, service.FinishBackupCreation(backup.ID, false))

	receiverVolumes := service.List(receiverProject, app.ListFilter{})
	primaryVolumes := service.List(seed.ProjectID, app.ListFilter{})
	_, crossVolumeErr := service.Get(seed.ProjectID, owned.ID)
	_, crossSeedErr := service.Get(receiverProject, seed.ID)
	_, crossSnapshotErr := service.GetSnapshot(seed.ProjectID, snapshot.ID)
	_, crossBackupErr := service.GetBackup(seed.ProjectID, backup.ID)
	_, crossMetadataErr := service.GetVolumeMetadata(seed.ProjectID, owned.ID)
	_, crossUpdateErr := service.Update(seed.ProjectID, owned.ID, app.TextUpdate{})
	crossExtendErr := service.StartExtension(seed.ProjectID, owned.ID, 2)
	crossDeleteErr := service.Delete(seed.ProjectID, owned.ID)
	_, crossSnapshotCreateErr := service.CreateSnapshot(seed.ProjectID, owned.ID, app.SnapshotCreateOptions{})
	_, crossCloneErr := service.CreateFromVolume(seed.ProjectID, seed.UserID, owned.ID, "", "", 0, nil)
	_, crossFromSnapshotErr := service.CreateFromSnapshot(seed.ProjectID, seed.UserID, snapshot.ID, "", "", 0)
	_, crossBackupCreateErr := service.CreateBackup(seed.ProjectID, owned.ID, app.BackupCreateOptions{})
	_, crossRestoreErr := service.RestoreBackup(seed.ProjectID, seed.UserID, backup.ID, "", nil)
	_, crossRestoreTargetErr := service.RestoreBackup(receiverProject, receiverUser, backup.ID, seed.ID, nil)
	_, crossTransferErr := service.CreateTransfer(seed.ProjectID, owned.ID, nil)
	_, crossMarkerErr := service.ListPage(seed.ProjectID, app.ListFilter{}, &owned.ID, -1)

	require.Len(t, receiverVolumes, 1)
	assert.Equal(t, owned.ID, receiverVolumes[0].ID)
	assert.Equal(t, receiverProject, receiverVolumes[0].ProjectID)
	assert.Equal(t, receiverUser, receiverVolumes[0].UserID)
	require.Len(t, primaryVolumes, 1)
	assert.Equal(t, seed.ID, primaryVolumes[0].ID)
	assert.Equal(t, receiverProject, snapshot.ProjectID)
	assert.Equal(t, receiverProject, backup.ProjectID)
	assert.Empty(t, service.ListSnapshots(seed.ProjectID))
	assert.Len(t, service.ListSnapshots(receiverProject), 1)
	assert.ErrorIs(t, crossVolumeErr, app.ErrVolumeNotFound)
	assert.ErrorIs(t, crossSeedErr, app.ErrVolumeNotFound)
	assert.ErrorIs(t, crossSnapshotErr, app.ErrSnapshotNotFound)
	assert.ErrorIs(t, crossBackupErr, app.ErrBackupNotFound)
	assert.ErrorIs(t, crossMetadataErr, app.ErrVolumeNotFound)
	assert.ErrorIs(t, crossUpdateErr, app.ErrVolumeNotFound)
	assert.ErrorIs(t, crossExtendErr, app.ErrVolumeNotFound)
	assert.ErrorIs(t, crossDeleteErr, app.ErrVolumeNotFound)
	assert.ErrorIs(t, crossSnapshotCreateErr, app.ErrVolumeNotFound)
	assert.ErrorIs(t, crossCloneErr, app.ErrVolumeSourceNotFound)
	assert.ErrorIs(t, crossFromSnapshotErr, app.ErrSnapshotNotFound)
	assert.ErrorIs(t, crossBackupCreateErr, app.ErrVolumeNotFound)
	assert.ErrorIs(t, crossRestoreErr, app.ErrBackupNotFound)
	assert.ErrorIs(t, crossRestoreTargetErr, app.ErrVolumeNotFound)
	assert.ErrorIs(t, crossTransferErr, app.ErrVolumeNotFound)
	assert.ErrorIs(t, crossMarkerErr, app.ErrVolumeNotFound)
}

func TestVolumeTypesAndZonesAreSharedByProjects(t *testing.T) {
	service := newOwnershipFixture().service

	types, err := service.ListVolumeTypesPage(nil, -1)
	require.NoError(t, err)
	created, err := service.CreateVolume(receiverProject, receiverUser, app.CreateRequest{
		VolumeType: app.PerformanceVolumeTypeName, Size: 1,
	})

	require.NoError(t, err)
	assert.Len(t, types.Types, 2)
	assert.Equal(t, app.PerformanceVolumeTypeName, app.VolumeTypeName(created.TypeID))
	assert.Equal(t, []domain.AvailabilityZone{{Name: app.StandardAvailabilityZone, Available: true}},
		service.ListAvailabilityZones())
}

// https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/transfer/api.py#L235-L351
// https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/db/api.py#L6996-L7061
func TestTransferAcceptMovesTheVolumeAndSnapshotsButNotBackups(t *testing.T) {
	fixture := newOwnershipFixture()
	service, seed := fixture.service, fixture.seed
	snapshot, err := service.CreateSnapshot(seed.ProjectID, seed.ID, app.SnapshotCreateOptions{})
	require.NoError(t, err)
	require.NoError(t, service.FinishSnapshotCreation(snapshot.ID, false))
	backup, err := service.CreateBackup(seed.ProjectID, seed.ID, app.BackupCreateOptions{})
	require.NoError(t, err)
	require.NoError(t, service.FinishBackupCreation(backup.ID, false))
	created, err := service.CreateTransfer(seed.ProjectID, seed.ID, nil)
	require.NoError(t, err)
	_, receiverReadErr := service.GetTransfer(receiverProject, created.Transfer.ID)
	fixture.tick()

	accepted, err := service.AcceptTransfer(receiverProject, receiverUser, created.Transfer.ID, created.AuthKey)
	require.NoError(t, err)
	moved, movedErr := service.Get(receiverProject, seed.ID)
	movedSnapshot, movedSnapshotErr := service.GetSnapshot(receiverProject, snapshot.ID)
	_, donorVolumeErr := service.Get(seed.ProjectID, seed.ID)
	_, donorSnapshotErr := service.GetSnapshot(seed.ProjectID, snapshot.ID)
	keptBackup, keptBackupErr := service.GetBackup(seed.ProjectID, backup.ID)
	_, receiverBackupErr := service.GetBackup(receiverProject, backup.ID)
	_, donorTransferErr := service.GetTransfer(seed.ProjectID, created.Transfer.ID)

	assert.ErrorIs(t, receiverReadErr, app.ErrTransferNotFound)
	assert.Equal(t, created.Transfer, accepted)
	require.NoError(t, movedErr)
	assert.Equal(t, receiverProject, moved.ProjectID)
	assert.Equal(t, receiverUser, moved.UserID)
	assert.Equal(t, domain.StateAvailable, moved.State)
	assert.Equal(t, fixture.now, moved.UpdatedAt)
	require.NoError(t, movedSnapshotErr)
	assert.Equal(t, receiverProject, movedSnapshot.ProjectID)
	assert.Equal(t, fixture.now, movedSnapshot.UpdatedAt)
	assert.ErrorIs(t, donorVolumeErr, app.ErrVolumeNotFound)
	assert.ErrorIs(t, donorSnapshotErr, app.ErrSnapshotNotFound)
	require.NoError(t, keptBackupErr)
	assert.Equal(t, seed.ProjectID, keptBackup.ProjectID)
	assert.ErrorIs(t, receiverBackupErr, app.ErrBackupNotFound)
	assert.ErrorIs(t, donorTransferErr, app.ErrTransferNotFound)
	assert.Empty(t, service.List(seed.ProjectID, app.ListFilter{}))
	assert.Empty(t, service.ListSnapshots(seed.ProjectID))
	assert.Len(t, service.List(receiverProject, app.ListFilter{}), 1)
}

// https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/db/api.py#L6960-L7048
func TestTransferAcceptRefusesASnapshotThatIsNotAvailable(t *testing.T) {
	fixture := newOwnershipFixture()
	service, seed := fixture.service, fixture.seed
	snapshot, err := service.CreateSnapshot(seed.ProjectID, seed.ID, app.SnapshotCreateOptions{})
	require.NoError(t, err)
	require.NoError(t, service.FinishSnapshotCreation(snapshot.ID, false))
	created, err := service.CreateTransfer(seed.ProjectID, seed.ID, nil)
	require.NoError(t, err)
	_, err = service.CreateBackup(seed.ProjectID, seed.ID, app.BackupCreateOptions{SnapshotID: snapshot.ID})
	require.NoError(t, err)
	fixture.tick()

	_, acceptErr := service.AcceptTransfer(receiverProject, receiverUser, created.Transfer.ID, created.AuthKey)
	held := fixture.volume(t, seed.ID)
	_, readErr := service.GetTransfer(seed.ProjectID, created.Transfer.ID)

	assert.Equal(t, app.TransferAcceptSnapshotStateError{
		TransferID: created.Transfer.ID, SnapshotID: snapshot.ID,
	}, acceptErr)
	assert.Equal(t, seed.ProjectID, held.ProjectID)
	assert.Equal(t, domain.StateAwaitingTransfer, held.State)
	assert.Equal(t, fixture.now, held.UpdatedAt)
	assert.NoError(t, readErr)
}

// https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/db/api.py#L6543-L6578
func TestIncrementalBackupIgnoresAnotherProjectsBackups(t *testing.T) {
	fixture := newOwnershipFixture()
	service, seed := fixture.service, fixture.seed
	backup, err := service.CreateBackup(seed.ProjectID, seed.ID, app.BackupCreateOptions{})
	require.NoError(t, err)
	require.NoError(t, service.FinishBackupCreation(backup.ID, false))
	created, err := service.CreateTransfer(seed.ProjectID, seed.ID, nil)
	require.NoError(t, err)
	_, err = service.AcceptTransfer(receiverProject, receiverUser, created.Transfer.ID, created.AuthKey)
	require.NoError(t, err)

	_, incrementalErr := service.CreateBackup(receiverProject, seed.ID, app.BackupCreateOptions{Incremental: true})

	assert.ErrorIs(t, incrementalErr, app.ErrNoIncrementalParent)
}
