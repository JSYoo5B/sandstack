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

func TestExtensionPublishesNewSizeOnlyAfterSuccess(t *testing.T) {
	start := time.Date(2026, time.November, 25, 1, 2, 3, 0, time.UTC)
	now := start.In(time.FixedZone("fixture", 3600))
	seed := domain.Volume{
		ID: fixtureID, ProjectID: "project", Size: 5, State: domain.StateAvailable, CreatedAt: start,
	}
	service := app.New(seed, func() time.Time { return now }, nil)
	assert.ErrorIs(t, service.StartExtension("other", seed.ID, 8), app.ErrVolumeNotFound)
	assert.ErrorIs(t, service.StartExtension(seed.ProjectID, "missing", 8), app.ErrVolumeNotFound)
	assert.ErrorIs(t, service.FinishExtension("missing", false), app.ErrVolumeNotFound)
	assert.ErrorIs(t, service.FinishExtension(seed.ID, false), app.ErrVolumeStateConflict)
	for _, size := range []int{-1, 0, seed.Size - 1, seed.Size} {
		assert.ErrorIs(t, service.StartExtension(seed.ProjectID, seed.ID, size), app.ErrInvalidExtensionSize)
	}
	assert.Equal(t, []domain.Volume{seed}, service.List(seed.ProjectID))

	now = now.Add(time.Minute)
	require.NoError(t, service.StartExtension(seed.ProjectID, seed.ID, 8))
	pending, err := service.Get(seed.ProjectID, seed.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.StateExtending, pending.State)
	assert.Equal(t, seed.Size, pending.Size)
	assert.Equal(t, start, pending.CreatedAt)
	assert.Equal(t, start.Add(time.Minute), pending.UpdatedAt)
	assert.ErrorIs(t, service.StartExtension(seed.ProjectID, seed.ID, 9), app.ErrVolumeStateConflict)
	assert.ErrorIs(t, service.Delete(seed.ProjectID, seed.ID), app.ErrVolumeStateConflict)

	now = now.Add(time.Minute)
	assert.ErrorIs(t, service.SetFailed(seed.ID, true), app.ErrVolumeStateConflict)
	require.NoError(t, service.SetFailed(seed.ID, false))
	assert.Equal(t, []domain.Volume{pending}, service.List(seed.ProjectID))
	require.NoError(t, service.FinishExtension(seed.ID, false))
	assert.ErrorIs(t, service.FinishExtension(seed.ID, false), app.ErrVolumeStateConflict)
	finished, err := service.Get(seed.ProjectID, seed.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.StateAvailable, finished.State)
	assert.Equal(t, 8, finished.Size)
	assert.Equal(t, start.Add(2*time.Minute), finished.UpdatedAt)
}

func TestExtensionFailureKeepsSizeAndAllowsClearingOrDeletion(t *testing.T) {
	seed := domain.Volume{ID: fixtureID, ProjectID: "project", Size: 5, State: domain.StateAvailable}
	service := app.New(seed, time.Now, func() string { return createdID })
	attached, err := service.Attach(seed.ProjectID, "server", seed.ID, "")
	require.NoError(t, err)
	assert.ErrorIs(t, service.StartExtension(seed.ProjectID, seed.ID, 8), app.ErrVolumeStateConflict)
	require.NoError(t, service.Detach(seed.ProjectID, attached.ServerID, seed.ID))

	require.NoError(t, service.StartExtension(seed.ProjectID, seed.ID, 8))
	require.NoError(t, service.FinishExtension(seed.ID, true))
	assert.ErrorIs(t, service.FinishExtension(seed.ID, false), app.ErrVolumeStateConflict)
	failed, err := service.Get(seed.ProjectID, seed.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.StateErrorExtending, failed.State)
	assert.Equal(t, seed.Size, failed.Size)
	assert.ErrorIs(t, service.StartExtension(seed.ProjectID, seed.ID, 9), app.ErrVolumeStateConflict)
	require.NoError(t, service.SetFailed(seed.ID, false))
	cleared, err := service.Get(seed.ProjectID, seed.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.StateAvailable, cleared.State)
	assert.Equal(t, seed.Size, cleared.Size)

	require.NoError(t, service.StartExtension(seed.ProjectID, seed.ID, 9))
	require.NoError(t, service.FinishExtension(seed.ID, true))
	require.NoError(t, service.Delete(seed.ProjectID, seed.ID))
	assert.Empty(t, service.List(seed.ProjectID))

	service.Reset()
	require.NoError(t, service.StartExtension(seed.ProjectID, seed.ID, 8))
	service.Reset()
	assert.Equal(t, []domain.Volume{seed}, service.List(seed.ProjectID))
	assert.ErrorIs(t, service.FinishExtension(seed.ID, false), app.ErrVolumeStateConflict)
}

func TestConcurrentExtensionsKeepOnlyAcceptedSize(t *testing.T) {
	seed := domain.Volume{ID: fixtureID, ProjectID: "project", Size: 5, State: domain.StateAvailable}
	service := app.New(seed, time.Now, nil)
	var first, second error
	var wait sync.WaitGroup
	wait.Go(func() { first = service.StartExtension(seed.ProjectID, seed.ID, 8) })
	wait.Go(func() { second = service.StartExtension(seed.ProjectID, seed.ID, 9) })
	wait.Wait()

	wantSize := 8
	if first == nil {
		assert.ErrorIs(t, second, app.ErrVolumeStateConflict)
	} else {
		assert.ErrorIs(t, first, app.ErrVolumeStateConflict)
		require.NoError(t, second)
		wantSize = 9
	}
	require.NoError(t, service.FinishExtension(seed.ID, false))
	volume, err := service.Get(seed.ProjectID, seed.ID)
	require.NoError(t, err)
	assert.Equal(t, wantSize, volume.Size)
}
