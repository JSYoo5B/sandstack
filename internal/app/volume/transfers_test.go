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

func newTransferFixture(seed domain.Volume) *backupFixture {
	fixture := newBackupFixture(seed)
	keys := 0
	fixture.service.WithTransferKeys(func() string {
		keys++
		return fmt.Sprintf("%016x", keys)
	})
	return fixture
}

// https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/transfer/api.py#L63-L104
// https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/transfer/api.py#L131-L197
// https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/db/api.py#L6905-L6957
// https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/volume/api.py#L476-L518
func TestTransferCreationHoldsVolumeUntilDeleted(t *testing.T) {
	fixture := newTransferFixture(domain.Volume{ID: fixtureID, ProjectID: "project", Size: 1, State: domain.StateAvailable})
	service, seed := fixture.service, fixture.seed
	start := fixture.now

	_, foreignErr := service.CreateTransfer("foreign", seed.ID, nil)
	_, missingErr := service.CreateTransfer(seed.ProjectID, "missing", nil)
	unkeyed := app.New(seed, func() time.Time { return start }, func() string { return transferIDFor(1) })
	_, keyErr := unkeyed.CreateTransfer(seed.ProjectID, seed.ID, nil)

	assert.ErrorIs(t, foreignErr, app.ErrVolumeNotFound)
	assert.ErrorIs(t, missingErr, app.ErrVolumeNotFound)
	assert.ErrorIs(t, keyErr, app.ErrTransferKeyGeneration)
	assert.Equal(t, domain.StateAvailable, fixture.volume(t, seed.ID).State)

	snapshot, err := service.CreateSnapshot(seed.ProjectID, seed.ID, app.SnapshotCreateOptions{})
	require.NoError(t, err)
	_, snapshotErr := service.CreateTransfer(seed.ProjectID, seed.ID, nil)
	require.NoError(t, service.FinishSnapshotCreation(snapshot.ID, false))
	fixture.tick()

	assert.Equal(t, app.TransferSnapshotStateError{SnapshotID: snapshot.ID}, snapshotErr)

	created, err := service.CreateTransfer(seed.ProjectID, seed.ID, text("handoff"))
	require.NoError(t, err)
	held := fixture.volume(t, seed.ID)
	_, repeatErr := service.CreateTransfer(seed.ProjectID, seed.ID, nil)
	read, err := service.GetTransfer(seed.ProjectID, created.Transfer.ID)
	require.NoError(t, err)
	_, foreignReadErr := service.GetTransfer("foreign", created.Transfer.ID)

	assert.Equal(t, "0000000000000001", created.AuthKey)
	assert.Equal(t, domain.Transfer{
		ID:        created.Transfer.ID,
		ProjectID: seed.ProjectID,
		VolumeID:  seed.ID,
		Name:      domain.NullableText{Value: "handoff", Valid: true},
		CreatedAt: fixture.now,
	}, created.Transfer)
	assert.Equal(t, created.Transfer, read)
	assert.Equal(t, []domain.Transfer{read}, service.ListTransfers(seed.ProjectID))
	assert.Empty(t, service.ListTransfers("foreign"))
	assert.Equal(t, domain.StateAwaitingTransfer, held.State)
	assert.Equal(t, fixture.now, held.UpdatedAt)
	assert.ErrorIs(t, repeatErr, app.ErrTransferVolumeState)
	assert.ErrorIs(t, foreignReadErr, app.ErrTransferNotFound)

	deleteErr := service.Delete(seed.ProjectID, seed.ID)
	extendErr := service.StartExtension(seed.ProjectID, seed.ID, 2)
	retypeErr := service.Retype(seed.ProjectID, seed.ID, "performance")
	_, attachErr := service.Attach(seed.ProjectID, "server", seed.ID, "")
	_, snapshotCreateErr := service.CreateSnapshot(seed.ProjectID, seed.ID, app.SnapshotCreateOptions{Force: true})
	_, backupErr := service.CreateBackup(seed.ProjectID, seed.ID, app.BackupCreateOptions{Force: true})
	_, cloneErr := service.CreateVolume(seed.ProjectID, seed.UserID, app.CreateRequest{SourceVolumeID: seed.ID})
	faultErr := service.SetFailed(seed.ID, true)

	assert.ErrorIs(t, deleteErr, app.ErrVolumeHasSnapshots)
	assert.ErrorIs(t, extendErr, app.ErrVolumeStateConflict)
	assert.ErrorIs(t, retypeErr, app.ErrVolumeStateConflict)
	assert.ErrorIs(t, attachErr, app.ErrVolumeStateConflict)
	assert.ErrorIs(t, snapshotCreateErr, app.ErrVolumeStateConflict)
	assert.ErrorIs(t, backupErr, app.ErrBackupVolumeState)
	assert.ErrorIs(t, cloneErr, app.ErrVolumeSourceState)
	assert.ErrorIs(t, faultErr, app.ErrVolumeStateConflict)
	assert.Equal(t, held, fixture.volume(t, seed.ID))

	require.NoError(t, service.DeleteSnapshot(seed.ProjectID, snapshot.ID))
	deleteHeldErr := service.Delete(seed.ProjectID, seed.ID)
	fixture.tick()
	foreignCancelErr := service.DeleteTransfer("foreign", created.Transfer.ID)
	require.NoError(t, service.DeleteTransfer(seed.ProjectID, created.Transfer.ID))
	released := fixture.volume(t, seed.ID)
	_, deletedReadErr := service.GetTransfer(seed.ProjectID, created.Transfer.ID)
	repeatCancelErr := service.DeleteTransfer(seed.ProjectID, created.Transfer.ID)

	assert.ErrorIs(t, deleteHeldErr, app.ErrVolumeStateConflict)
	assert.ErrorIs(t, foreignCancelErr, app.ErrTransferNotFound)
	assert.Equal(t, domain.StateAvailable, released.State)
	assert.Equal(t, fixture.now, released.UpdatedAt)
	assert.ErrorIs(t, deletedReadErr, app.ErrTransferNotFound)
	assert.ErrorIs(t, repeatCancelErr, app.ErrTransferNotFound)
	assert.Empty(t, service.ListTransfers(seed.ProjectID))
}

// https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/transfer/api.py#L235-L351
// https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/db/api.py#L6996-L7061
func TestTransferAcceptanceRequiresTheAuthKey(t *testing.T) {
	fixture := newTransferFixture(domain.Volume{ID: fixtureID, ProjectID: "project", Size: 1, State: domain.StateAvailable})
	service, seed := fixture.service, fixture.seed
	created, err := service.CreateTransfer(seed.ProjectID, seed.ID, nil)
	require.NoError(t, err)
	held := fixture.volume(t, seed.ID)
	fixture.tick()

	_, wrongKeyErr := service.AcceptTransfer(seed.ProjectID, seed.UserID, created.Transfer.ID, "0000000000000002")
	_, emptyKeyErr := service.AcceptTransfer(seed.ProjectID, seed.UserID, created.Transfer.ID, "")
	_, missingErr := service.AcceptTransfer(seed.ProjectID, seed.UserID, "missing", created.AuthKey)

	assert.ErrorIs(t, wrongKeyErr, app.ErrTransferAuthKey)
	assert.ErrorIs(t, emptyKeyErr, app.ErrTransferAuthKey)
	assert.ErrorIs(t, missingErr, app.ErrTransferNotFound)
	assert.Equal(t, held, fixture.volume(t, seed.ID))
	assert.Len(t, service.ListTransfers(seed.ProjectID), 1)

	accepted, err := service.AcceptTransfer(seed.ProjectID, seed.UserID, created.Transfer.ID, created.AuthKey)
	require.NoError(t, err)
	owned := fixture.volume(t, seed.ID)
	_, repeatErr := service.AcceptTransfer(seed.ProjectID, seed.UserID, created.Transfer.ID, created.AuthKey)

	assert.Equal(t, created.Transfer, accepted)
	assert.Equal(t, domain.StateAvailable, owned.State)
	assert.Equal(t, seed.ProjectID, owned.ProjectID)
	assert.Equal(t, fixture.now, owned.UpdatedAt)
	assert.ErrorIs(t, repeatErr, app.ErrTransferNotFound)
	assert.Empty(t, service.ListTransfers(seed.ProjectID))
}

func TestTransferResetRestoresTheVolume(t *testing.T) {
	fixture := newTransferFixture(domain.Volume{ID: fixtureID, ProjectID: "project", Size: 1, State: domain.StateAvailable})
	service, seed := fixture.service, fixture.seed
	created, err := service.CreateTransfer(seed.ProjectID, seed.ID, nil)
	require.NoError(t, err)

	service.Reset()

	_, readErr := service.GetTransfer(seed.ProjectID, created.Transfer.ID)
	assert.ErrorIs(t, readErr, app.ErrTransferNotFound)
	assert.Empty(t, service.ListTransfers(seed.ProjectID))
	assert.Equal(t, seed, fixture.volume(t, seed.ID))
}

func transferIDFor(n int) string {
	return fmt.Sprintf("%08x-0000-4000-8000-000000000000", n)
}
