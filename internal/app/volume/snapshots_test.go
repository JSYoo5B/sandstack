package volume_test

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	app "github.com/JSYoo5B/sandstack/internal/app/volume"
	domain "github.com/JSYoo5B/sandstack/internal/domain/volume"
)

func TestSnapshotCreationAndDeletionProtectSourceVolume(t *testing.T) {
	start := time.Date(2026, time.November, 25, 0, 0, 0, 0, time.UTC)
	now := start.In(time.FixedZone("fixture", 3600))
	seed := domain.Volume{ID: fixtureID, ProjectID: "project", Size: 5, State: domain.StateAvailable}
	service := app.New(seed, func() time.Time { return now }, func() string { return createdID })

	_, err := service.CreateSnapshot("foreign", seed.ID, "", "")
	assert.ErrorIs(t, err, app.ErrVolumeNotFound)
	_, err = service.CreateSnapshot(seed.ProjectID, "unknown", "", "")
	assert.ErrorIs(t, err, app.ErrVolumeNotFound)

	snapshot, err := service.CreateSnapshot(seed.ProjectID, seed.ID, "checkpoint", "initial state")
	require.NoError(t, err)
	assert.Equal(t, domain.Snapshot{
		ID:          createdID,
		ProjectID:   seed.ProjectID,
		VolumeID:    seed.ID,
		Name:        "checkpoint",
		Description: "initial state",
		Size:        seed.Size,
		CreatedAt:   start,
		State:       domain.StateCreating,
	}, snapshot)
	assert.Equal(t, []domain.Snapshot{snapshot}, service.ListSnapshots(seed.ProjectID))
	assert.Empty(t, service.ListSnapshots("foreign"))
	_, err = service.GetSnapshot("foreign", snapshot.ID)
	assert.ErrorIs(t, err, app.ErrSnapshotNotFound)
	assert.ErrorIs(t, service.DeleteSnapshot("foreign", snapshot.ID), app.ErrSnapshotNotFound)
	assert.ErrorIs(t, service.DeleteSnapshot(seed.ProjectID, snapshot.ID), app.ErrSnapshotStateConflict)
	assert.ErrorIs(t, service.Delete(seed.ProjectID, seed.ID), app.ErrVolumeHasSnapshots)

	now = now.Add(time.Minute)
	require.NoError(t, service.FinishSnapshotCreation(snapshot.ID, false))
	available, err := service.GetSnapshot(seed.ProjectID, snapshot.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.StateAvailable, available.State)
	assert.Equal(t, start.Add(time.Minute), available.UpdatedAt)
	assert.ErrorIs(t, service.FinishSnapshotCreation(snapshot.ID, true), app.ErrSnapshotStateConflict)

	now = now.Add(time.Minute)
	require.NoError(t, service.FinishSnapshotCreation(snapshot.ID, false))
	assert.Equal(t, []domain.Snapshot{available}, service.ListSnapshots(seed.ProjectID))
	require.NoError(t, service.DeleteSnapshot(seed.ProjectID, snapshot.ID))
	assert.ErrorIs(t, service.FinishSnapshotCreation(snapshot.ID, false), app.ErrSnapshotNotFound)
	assert.NoError(t, service.Delete(seed.ProjectID, seed.ID))
}

func TestFailedSnapshotCanBeDeletedAndResetRestoresEmptyList(t *testing.T) {
	seed := domain.Volume{ID: fixtureID, ProjectID: "project", Size: 5, State: domain.StateAvailable}
	service := app.New(seed, time.Now, func() string { return createdID })
	require.NoError(t, service.SetFailed(seed.ID, true))
	_, err := service.CreateSnapshot(seed.ProjectID, seed.ID, "", "")
	assert.ErrorIs(t, err, app.ErrVolumeStateConflict)
	require.NoError(t, service.SetFailed(seed.ID, false))
	snapshot, err := service.CreateSnapshot(seed.ProjectID, seed.ID, "", "")
	require.NoError(t, err)

	require.NoError(t, service.FinishSnapshotCreation(snapshot.ID, true))
	failed, err := service.GetSnapshot(seed.ProjectID, snapshot.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.StateError, failed.State)
	require.NoError(t, service.FinishSnapshotCreation(snapshot.ID, true))
	assert.ErrorIs(t, service.FinishSnapshotCreation(snapshot.ID, false), app.ErrSnapshotStateConflict)
	assert.Equal(t, []domain.Snapshot{failed}, service.ListSnapshots(seed.ProjectID))
	assert.ErrorIs(t, service.Delete(seed.ProjectID, seed.ID), app.ErrVolumeHasSnapshots)
	require.NoError(t, service.DeleteSnapshot(seed.ProjectID, snapshot.ID))
	_, err = service.CreateSnapshot(seed.ProjectID, seed.ID, "", "")
	require.NoError(t, err)

	service.Reset()
	assert.Empty(t, service.ListSnapshots(seed.ProjectID))
	assert.Equal(t, []domain.Volume{seed}, service.List(seed.ProjectID, app.ListFilter{}))
}

func TestConcurrentSnapshotCreateAndSourceDeletePreserveOwnership(t *testing.T) {
	seed := domain.Volume{ID: fixtureID, ProjectID: "project", State: domain.StateAvailable}
	const attempts = 16
	for range attempts {
		service := app.New(seed, time.Now, func() string { return createdID })
		var createErr, deleteErr error
		var wait sync.WaitGroup
		wait.Go(func() {
			_, createErr = service.CreateSnapshot(seed.ProjectID, seed.ID, "", "")
		})
		wait.Go(func() { deleteErr = service.Delete(seed.ProjectID, seed.ID) })
		wait.Wait()

		if createErr == nil {
			assert.ErrorIs(t, deleteErr, app.ErrVolumeHasSnapshots)
			assert.Len(t, service.ListSnapshots(seed.ProjectID), 1)
			assert.Equal(t, []domain.Volume{seed}, service.List(seed.ProjectID, app.ListFilter{}))
		} else {
			assert.ErrorIs(t, createErr, app.ErrVolumeNotFound)
			assert.NoError(t, deleteErr)
			assert.Empty(t, service.ListSnapshots(seed.ProjectID))
			assert.Empty(t, service.List(seed.ProjectID, app.ListFilter{}))
		}
	}
}
