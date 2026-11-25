package compute_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	app "github.com/JSYoo5B/sandstack/internal/app/compute"
	domain "github.com/JSYoo5B/sandstack/internal/domain/compute"
)

func TestServerMetadatumActionsPreserveOtherKeysAndDetailControls(t *testing.T) {
	start := time.Date(2026, time.November, 25, 1, 2, 3, 0, time.UTC)
	now := start.In(time.FixedZone("fixture", 3600))
	seed := domain.Server{
		ID: "server", ProjectID: "project", UpdatedAt: start,
		Metadata: map[string]string{"item": "", "other": "kept"},
	}
	service := app.New(seed, func() time.Time { return now }, nil)
	require.NoError(t, service.SetStaleDetail(seed.ID, true))
	require.NoError(t, service.SetUnavailableDetail(seed.ID, true))
	require.NoError(t, service.SetFault(seed.ID, true))

	value, err := service.GetMetadatum(seed.ProjectID, seed.ID, "item")
	require.NoError(t, err)
	assert.Empty(t, value)
	_, err = service.GetMetadatum("foreign", seed.ID, "item")
	assert.ErrorIs(t, err, app.ErrServerNotFound)
	_, err = service.GetMetadatum(seed.ProjectID, seed.ID, "missing")
	assert.ErrorIs(t, err, app.ErrServerMetadatumNotFound)
	assert.ErrorIs(t, service.PutMetadatum(seed.ProjectID, seed.ID, "item", "value"), app.ErrServerMetadataState)
	assert.ErrorIs(t, service.DeleteMetadatum(seed.ProjectID, "missing", "item"), app.ErrServerNotFound)
	assert.ErrorIs(t, service.DeleteMetadatum(seed.ProjectID, seed.ID, "missing"), app.ErrServerMetadatumNotFound)
	assert.ErrorIs(t, service.DeleteMetadatum(seed.ProjectID, seed.ID, "item"), app.ErrServerMetadataState)

	require.NoError(t, service.SetFault(seed.ID, false))
	now = now.Add(time.Minute)
	require.NoError(t, service.PutMetadatum(seed.ProjectID, seed.ID, "item", "value"))
	updatedAt := now.UTC()
	put := service.List(seed.ProjectID, app.ListFilter{})
	require.Len(t, put, 1)
	assert.Equal(t, map[string]string{"item": "value", "other": "kept"}, put[0].Metadata)
	assert.Equal(t, updatedAt, put[0].UpdatedAt)

	now = now.Add(time.Minute)
	require.NoError(t, service.DeleteMetadatum(seed.ProjectID, seed.ID, "item"))
	assert.ErrorIs(t, service.DeleteMetadatum(seed.ProjectID, seed.ID, "item"), app.ErrServerMetadatumNotFound)
	live := service.List(seed.ProjectID, app.ListFilter{})
	require.Len(t, live, 1)
	assert.Equal(t, map[string]string{"other": "kept"}, live[0].Metadata)
	assert.Equal(t, updatedAt, live[0].UpdatedAt)
	assert.Equal(t, map[string]string{"item": "value", "other": "kept"}, put[0].Metadata)
	_, err = service.Get(seed.ProjectID, seed.ID)
	assert.ErrorIs(t, err, app.ErrDetailUnavailable)
	stale, err := service.Get(seed.ProjectID, seed.ID)
	require.NoError(t, err)
	assert.Equal(t, seed, stale.Server)
}
