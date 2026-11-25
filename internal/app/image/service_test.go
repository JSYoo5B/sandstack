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
	service := app.New(seed, func() time.Time { return clock }, "", nil)

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
	service := app.New(seed, time.Now, "", nil)

	_, err := service.Get("another-project", seed.ID)
	assert.ErrorIs(t, err, app.ErrImageNotFound)
	name := "not authorized"
	_, err = service.Update("another-project", seed.ID, app.ImageUpdate{Name: &name})
	assert.ErrorIs(t, err, app.ErrImageNotFound)
	assert.Empty(t, service.List("another-project", app.ListFilter{}))
	assert.Equal(t, []domain.Image{seed}, service.List(seed.ProjectID, app.ListFilter{}))
	_, err = service.Get(seed.ProjectID, "another-image")
	assert.ErrorIs(t, err, app.ErrImageNotFound)
	assert.ErrorIs(t, service.SetDeactivated("another-image", true), app.ErrImageNotFound)

	image, err := service.Get(seed.ProjectID, seed.ID)
	require.NoError(t, err)
	assert.Equal(t, seed, image)
}

func TestImageTagsDoNotShareSliceStorageWithCallers(t *testing.T) {
	seed := domain.Image{ID: "image", ProjectID: "project", Tags: []string{"seed-tag"}}
	service := app.New(seed, func() time.Time { return time.Time{} }, "", nil)
	seed.Tags[0] = "modified seed"

	tags := []string{"second", "first", "first"}
	updated, err := service.Update(seed.ProjectID, seed.ID, app.ImageUpdate{Tags: tags})
	require.NoError(t, err)
	assert.Equal(t, []string{"first", "second"}, updated.Tags)
	tags[0] = "modified request"
	updated.Tags[0] = "modified response"

	got, err := service.Get(seed.ProjectID, seed.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{"first", "second"}, got.Tags)
	got.Tags[0] = "modified detail"
	listed := service.List(seed.ProjectID, app.ListFilter{})
	require.Len(t, listed, 1)
	assert.Equal(t, []string{"first", "second"}, listed[0].Tags)
	listed[0].Tags[0] = "modified list"
	got, err = service.Get(seed.ProjectID, seed.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{"first", "second"}, got.Tags)

	service.Reset()
	restored, err := service.Get(seed.ProjectID, seed.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{"seed-tag"}, restored.Tags)
}
