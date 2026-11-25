package volume_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	app "github.com/JSYoo5B/sandstack/internal/app/volume"
	domain "github.com/JSYoo5B/sandstack/internal/domain/volume"
)

const (
	fixtureID = "00000010-0000-4000-8000-000000000000"
	createdID = "00000002-0000-4000-8000-000000000000"
)

func TestCreateCompleteAndDeleteVolume(t *testing.T) {
	start := time.Date(2026, time.November, 25, 0, 0, 0, 0, time.UTC)
	now := start.In(time.FixedZone("fixture", 9*3600))
	seed := domain.Volume{ID: fixtureID, ProjectID: "owner", UserID: "user", Size: 5, CreatedAt: start}
	service := app.New(seed, func() time.Time { return now }, func() string { return createdID })
	seed.State = domain.StateAvailable

	_, err := service.Create("foreign", "", "", 1)
	assert.ErrorIs(t, err, app.ErrVolumeNotFound)
	_, err = service.Create(seed.ProjectID, "", "", 0)
	assert.ErrorIs(t, err, app.ErrInvalidVolumeSize)
	_, err = service.Create(seed.ProjectID, "", "", -1)
	assert.ErrorIs(t, err, app.ErrInvalidVolumeSize)

	created, err := service.Create(seed.ProjectID, "scratch", "temporary data", 3)
	require.NoError(t, err)
	assert.Equal(t, domain.Volume{
		ID:          createdID,
		ProjectID:   seed.ProjectID,
		UserID:      seed.UserID,
		Name:        domain.NullableText{Value: "scratch", Valid: true},
		Description: domain.NullableText{Value: "temporary data", Valid: true},
		Size:        3,
		CreatedAt:   start,
		State:       domain.StateCreating,
	}, created)
	assert.Equal(t, []domain.Volume{created, seed}, service.List(seed.ProjectID))
	assert.ErrorIs(t, service.Delete("foreign", created.ID), app.ErrVolumeNotFound)
	assert.ErrorIs(t, service.Delete(seed.ProjectID, created.ID), app.ErrVolumeStateConflict)

	now = now.Add(time.Minute)
	require.NoError(t, service.SetFailed(created.ID, false))
	stillCreating, err := service.Get(seed.ProjectID, created.ID)
	require.NoError(t, err)
	assert.Equal(t, created, stillCreating)

	require.NoError(t, service.CompleteCreation(created.ID))
	available, err := service.Get(seed.ProjectID, created.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.StateAvailable, available.State)
	assert.Equal(t, start.Add(time.Minute), available.UpdatedAt)
	now = now.Add(time.Minute)
	require.NoError(t, service.CompleteCreation(created.ID))
	assert.Equal(t, []domain.Volume{available, seed}, service.List(seed.ProjectID))

	require.NoError(t, service.Delete(seed.ProjectID, created.ID))
	assert.ErrorIs(t, service.Delete(seed.ProjectID, created.ID), app.ErrVolumeNotFound)
	assert.ErrorIs(t, service.CompleteCreation(created.ID), app.ErrVolumeNotFound)
	assert.Equal(t, []domain.Volume{seed}, service.List(seed.ProjectID))
}

func TestCreatingVolumeFaultAndReset(t *testing.T) {
	start := time.Date(2026, time.November, 25, 0, 0, 0, 0, time.UTC)
	now := start
	seed := domain.Volume{ID: fixtureID, ProjectID: "owner", Size: 5, State: domain.StateAvailable}
	service := app.New(seed, func() time.Time { return now }, func() string { return createdID })
	created, err := service.Create(seed.ProjectID, "scratch", "", 1)
	require.NoError(t, err)

	now = now.Add(time.Minute)
	require.NoError(t, service.SetFailed(created.ID, true))
	failed, err := service.Get(seed.ProjectID, created.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.StateError, failed.State)
	assert.Equal(t, start.Add(time.Minute), failed.UpdatedAt)
	assert.ErrorIs(t, service.CompleteCreation(created.ID), app.ErrVolumeStateConflict)

	now = now.Add(time.Minute)
	require.NoError(t, service.SetFailed(created.ID, false))
	available, err := service.Get(seed.ProjectID, created.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.StateAvailable, available.State)
	assert.Equal(t, start.Add(2*time.Minute), available.UpdatedAt)
	require.NoError(t, service.SetFailed(created.ID, true))
	require.NoError(t, service.Delete(seed.ProjectID, created.ID))
	require.NoError(t, service.Delete(seed.ProjectID, seed.ID))
	_, err = service.Create(seed.ProjectID, "another volume", "", 1)
	require.NoError(t, err)

	service.Reset()
	assert.Equal(t, []domain.Volume{seed}, service.List(seed.ProjectID))
	_, err = service.Get(seed.ProjectID, created.ID)
	assert.ErrorIs(t, err, app.ErrVolumeNotFound)
}

func TestVolumeIDRetriesAndFailurePreserveExistingVolumes(t *testing.T) {
	seed := domain.Volume{ID: fixtureID, ProjectID: "owner", State: domain.StateAvailable}
	attempts := 0
	service := app.New(seed, time.Now, func() string {
		attempts++
		if attempts == 1 {
			return seed.ID
		}
		if attempts > 100 {
			return "00000003-0000-4000-8000-000000000000"
		}
		return createdID
	})
	require.NoError(t, service.Delete(seed.ProjectID, seed.ID))
	first, err := service.Create(seed.ProjectID, "original", "", 1)
	require.NoError(t, err)
	assert.Equal(t, createdID, first.ID)
	_, err = service.Create(seed.ProjectID, "replacement", "", 5)
	assert.ErrorIs(t, err, app.ErrVolumeIDGeneration)
	assert.Equal(t, []domain.Volume{first}, service.List(seed.ProjectID))

	for _, generate := range []func() string{nil, func() string { return "invalid-uuid" }} {
		service := app.New(seed, time.Now, generate)
		_, err := service.Create(seed.ProjectID, "invalid", "", 1)
		assert.ErrorIs(t, err, app.ErrVolumeIDGeneration)
		assert.Equal(t, []domain.Volume{seed}, service.List(seed.ProjectID))
	}
}
