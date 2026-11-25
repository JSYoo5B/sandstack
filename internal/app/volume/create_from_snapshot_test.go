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

func TestCreateFromSnapshotPreservesSizeAndProvenance(t *testing.T) {
	start := time.Date(2026, time.November, 25, 1, 2, 3, 0, time.UTC)
	now := start.In(time.FixedZone("fixture", 3600))
	seed := domain.Volume{
		ID: fixtureID, ProjectID: "project", UserID: "user", Size: 5, State: domain.StateAvailable,
	}
	id := 32
	service := app.New(seed, func() time.Time { return now }, func() string {
		id++
		return fmt.Sprintf("%08x-0000-4000-8000-000000000000", id)
	})
	snapshot, err := service.CreateSnapshot(seed.ProjectID, seed.ID, "checkpoint", "")
	require.NoError(t, err)
	_, err = service.CreateFromSnapshot(seed.ProjectID, snapshot.ID, "", "", 0)
	assert.ErrorIs(t, err, app.ErrSnapshotStateConflict)
	require.NoError(t, service.FinishSnapshotCreation(snapshot.ID, false))

	_, err = service.CreateFromSnapshot("foreign", snapshot.ID, "", "", 0)
	assert.ErrorIs(t, err, app.ErrSnapshotNotFound)
	_, err = service.CreateFromSnapshot(seed.ProjectID, "unknown", "", "", 0)
	assert.ErrorIs(t, err, app.ErrSnapshotNotFound)
	_, err = service.CreateFromSnapshot(seed.ProjectID, snapshot.ID, "", "", -1)
	assert.ErrorIs(t, err, app.ErrInvalidVolumeSize)
	_, err = service.CreateFromSnapshot(seed.ProjectID, snapshot.ID, "", "", seed.Size-1)
	assert.ErrorIs(t, err, app.ErrVolumeSmallerThanSnapshot)
	assert.Equal(t, []domain.Volume{seed}, service.List(seed.ProjectID, app.ListFilter{}))

	defaultSize, err := service.CreateFromSnapshot(
		seed.ProjectID, snapshot.ID, "clone", "recovered data", 0,
	)
	require.NoError(t, err)
	assert.NotEmpty(t, defaultSize.ID)
	assert.NotEqual(t, seed.ID, defaultSize.ID)
	assert.NotEqual(t, snapshot.ID, defaultSize.ID)
	assert.Equal(t, domain.Volume{
		ID:          defaultSize.ID,
		Name:        domain.NullableText{Value: "clone", Valid: true},
		Description: domain.NullableText{Value: "recovered data", Valid: true},
		ProjectID:   seed.ProjectID,
		UserID:      seed.UserID,
		SnapshotID:  snapshot.ID,
		Size:        seed.Size,
		CreatedAt:   start,
		State:       domain.StateCreating,
	}, defaultSize)
	sameSize, err := service.CreateFromSnapshot(seed.ProjectID, snapshot.ID, "", "", seed.Size)
	require.NoError(t, err)
	assert.Equal(t, seed.Size, sameSize.Size)
	larger, err := service.CreateFromSnapshot(seed.ProjectID, snapshot.ID, "", "", seed.Size+2)
	require.NoError(t, err)
	assert.Equal(t, seed.Size+2, larger.Size)
	assert.ElementsMatch(t, []domain.Volume{seed, defaultSize, sameSize, larger}, service.List(seed.ProjectID, app.ListFilter{}))
	source, err := service.GetSnapshot(seed.ProjectID, snapshot.ID)
	require.NoError(t, err)
	snapshot.State = domain.StateAvailable
	snapshot.UpdatedAt = start
	assert.Equal(t, snapshot, source)

	now = now.Add(time.Minute)
	require.NoError(t, service.CompleteCreation(defaultSize.ID))
	require.NoError(t, service.SetFailed(larger.ID, true))
	require.NoError(t, service.DeleteSnapshot(seed.ProjectID, snapshot.ID))
	completed, err := service.Get(seed.ProjectID, defaultSize.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.StateAvailable, completed.State)
	assert.Equal(t, snapshot.ID, completed.SnapshotID)
	assert.Equal(t, start.Add(time.Minute), completed.UpdatedAt)
	failed, err := service.Get(seed.ProjectID, larger.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.StateError, failed.State)
	assert.Equal(t, snapshot.ID, failed.SnapshotID)
	original, err := service.Get(seed.ProjectID, seed.ID)
	require.NoError(t, err)
	assert.Equal(t, seed, original)
	_, err = service.CreateFromSnapshot(seed.ProjectID, snapshot.ID, "", "", 0)
	assert.ErrorIs(t, err, app.ErrSnapshotNotFound)

	service.Reset()
	assert.Equal(t, []domain.Volume{seed}, service.List(seed.ProjectID, app.ListFilter{}))
}

func TestCreateFromSnapshotRejectsFailedSnapshot(t *testing.T) {
	seed := domain.Volume{ID: fixtureID, ProjectID: "project", Size: 5}
	service := app.New(seed, time.Now, func() string { return createdID })
	snapshot, err := service.CreateSnapshot(seed.ProjectID, seed.ID, "", "")
	require.NoError(t, err)
	require.NoError(t, service.FinishSnapshotCreation(snapshot.ID, true))

	_, err = service.CreateFromSnapshot(seed.ProjectID, snapshot.ID, "", "", 0)
	assert.ErrorIs(t, err, app.ErrSnapshotStateConflict)
}
