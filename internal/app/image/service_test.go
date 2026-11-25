package image_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	app "github.com/JSYoo5B/sandstack/internal/app/image"
	domain "github.com/JSYoo5B/sandstack/internal/domain/image"
)

func TestDeactivationUpdatesTimestampOnlyOnTransitionAndResetRestoresSeed(t *testing.T) {
	start := time.Date(2026, time.November, 25, 1, 2, 3, 0, time.UTC)
	clock := start.In(time.FixedZone("fixture", 3600))
	seed := domain.Image{
		ID:        "image",
		Name:      "fixture image",
		ProjectID: "project",
		CreatedAt: start.Add(-time.Minute),
		UpdatedAt: start,
	}
	service := app.New(seed, func() time.Time { return clock }, "")

	clock = clock.Add(time.Minute)
	require.NoError(t, service.SetDeactivated(seed.ID, true))
	deactivated, err := service.Get(seed.ProjectID, seed.ID)
	require.NoError(t, err)
	assert.True(t, deactivated.Deactivated)
	assert.Equal(t, start.Add(time.Minute), deactivated.UpdatedAt)
	assert.Equal(t, seed.CreatedAt, deactivated.CreatedAt)

	clock = clock.Add(time.Minute)
	require.NoError(t, service.SetDeactivated(seed.ID, true))
	retried, err := service.Get(seed.ProjectID, seed.ID)
	require.NoError(t, err)
	assert.Equal(t, deactivated, retried)

	require.NoError(t, service.SetDeactivated(seed.ID, false))
	active, err := service.Get(seed.ProjectID, seed.ID)
	require.NoError(t, err)
	assert.False(t, active.Deactivated)
	assert.Equal(t, start.Add(2*time.Minute), active.UpdatedAt)

	service.Reset()
	restored, err := service.Get(seed.ProjectID, seed.ID)
	require.NoError(t, err)
	assert.Equal(t, seed, restored)
}

func TestImageIsPrivateToSeededProject(t *testing.T) {
	seed := domain.Image{ID: "image", ProjectID: "project"}
	service := app.New(seed, time.Now, "")

	_, err := service.Get("another-project", seed.ID)
	assert.ErrorIs(t, err, app.ErrImageNotFound)
	_, err = service.Get(seed.ProjectID, "another-image")
	assert.ErrorIs(t, err, app.ErrImageNotFound)
	assert.ErrorIs(t, service.SetDeactivated("another-image", true), app.ErrImageNotFound)

	image, err := service.Get(seed.ProjectID, seed.ID)
	require.NoError(t, err)
	assert.Equal(t, seed, image)
}
