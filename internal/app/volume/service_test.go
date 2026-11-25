package volume_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	app "github.com/JSYoo5B/sandstack/internal/app/volume"
	domain "github.com/JSYoo5B/sandstack/internal/domain/volume"
)

func TestVolumeIsVisibleOnlyToOwner(t *testing.T) {
	seed := domain.Volume{ID: "volume", ProjectID: "owner"}
	service := app.New(seed, time.Now)

	assert.Empty(t, service.List("other-project"))
	_, err := service.Get("other-project", seed.ID)
	assert.ErrorIs(t, err, app.ErrVolumeNotFound)
	_, err = service.Get(seed.ProjectID, "other-volume")
	assert.ErrorIs(t, err, app.ErrVolumeNotFound)
	assert.Equal(t, []domain.Volume{seed}, service.List(seed.ProjectID))
}

func TestVolumeFaultTransitionsAndReset(t *testing.T) {
	created := time.Date(2026, time.November, 25, 0, 0, 0, 0, time.UTC)
	seed := domain.Volume{
		ID: "volume", ProjectID: "owner", CreatedAt: created, UpdatedAt: created,
	}
	local := time.FixedZone("fixture-zone", 9*60*60)
	now := created.Add(time.Minute).In(local)
	service := app.New(seed, func() time.Time { return now })

	assert.ErrorIs(t, service.SetFailed("other-volume", true), app.ErrVolumeNotFound)
	assert.Equal(t, []domain.Volume{seed}, service.List(seed.ProjectID))

	require.NoError(t, service.SetFailed(seed.ID, true))
	failed, err := service.Get(seed.ProjectID, seed.ID)
	require.NoError(t, err)
	assert.True(t, failed.Failed)
	assert.Equal(t, created, failed.CreatedAt)
	assert.Equal(t, now.UTC(), failed.UpdatedAt)

	now = now.Add(time.Minute)
	require.NoError(t, service.SetFailed(seed.ID, true))
	assert.Equal(t, []domain.Volume{failed}, service.List(seed.ProjectID))

	require.NoError(t, service.SetFailed(seed.ID, false))
	available, err := service.Get(seed.ProjectID, seed.ID)
	require.NoError(t, err)
	assert.False(t, available.Failed)
	assert.Equal(t, now.UTC(), available.UpdatedAt)

	service.Reset()
	assert.Equal(t, []domain.Volume{seed}, service.List(seed.ProjectID))
}
