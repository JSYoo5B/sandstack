package compute_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	app "github.com/JSYoo5B/sandstack/internal/app/compute"
	domain "github.com/JSYoo5B/sandstack/internal/domain/compute"
)

func TestUpdateNameReturnsLiveDetailAndPreservesReadControls(t *testing.T) {
	start := time.Date(2026, time.November, 25, 1, 2, 3, 0, time.UTC)
	seed := domain.Server{
		ID: "server", ProjectID: "project", Name: "original", UserID: "user", ImageID: "image",
		CreatedAt: start, UpdatedAt: start, Fault: &domain.Fault{Code: 500, CreatedAt: start},
	}
	now := start.Add(time.Minute).In(time.FixedZone("fixture", 3600))
	service := app.New(seed, func() time.Time { return now }, seededAttachments{{VolumeID: "volume"}})
	require.NoError(t, service.SetStaleDetail(seed.ID, true))
	require.NoError(t, service.SetUnavailableDetail(seed.ID, true))

	name := "renamed"
	_, err := service.UpdateName("foreign", seed.ID, &name)
	assert.ErrorIs(t, err, app.ErrServerNotFound)
	_, err = service.UpdateName(seed.ProjectID, "missing", &name)
	assert.ErrorIs(t, err, app.ErrServerNotFound)
	current, err := service.UpdateName(seed.ProjectID, seed.ID, nil)
	require.NoError(t, err)
	assert.Equal(t, seed, current.Server)

	updated, err := service.UpdateName(seed.ProjectID, seed.ID, &name)
	require.NoError(t, err)
	want := seed
	want.Name = name
	want.UpdatedAt = now.UTC()
	assert.Equal(t, want, updated.Server)
	assert.Equal(t, []string{"volume"}, updated.AttachedVolumeIDs)
	updated.Fault.Code = 499
	updated.AttachedVolumeIDs[0] = "mutated response"

	now = now.Add(time.Minute)
	repeated, err := service.UpdateName(seed.ProjectID, seed.ID, &name)
	require.NoError(t, err)
	assert.Equal(t, want, repeated.Server)
	assert.Equal(t, []string{"volume"}, repeated.AttachedVolumeIDs)
	assert.Equal(t, []app.ServerDetail{repeated}, service.List(seed.ProjectID, app.ListFilter{}))
	_, err = service.Get(seed.ProjectID, seed.ID)
	assert.ErrorIs(t, err, app.ErrDetailUnavailable)
	stale, err := service.Get(seed.ProjectID, seed.ID)
	require.NoError(t, err)
	assert.Equal(t, seed, stale.Server)
}
