package compute_test

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	app "github.com/JSYoo5B/sandstack/internal/app/compute"
	domain "github.com/JSYoo5B/sandstack/internal/domain/compute"
)

func TestReplaceServerMetadataKeepsSnapshotsAndDetailControls(t *testing.T) {
	start := time.Date(2026, time.November, 25, 1, 2, 3, 0, time.UTC)
	now := start.Add(time.Minute).In(time.FixedZone("fixture", 3600))
	seed := domain.Server{
		ID: "server", ProjectID: "project", Name: "fixture", CreatedAt: start, UpdatedAt: start,
		Metadata: map[string]string{"kept": "seed", "old": "remove"},
	}
	service := app.New(seed, func() time.Time { return now }, nil)
	seed.Metadata["kept"] = "changed caller seed"
	require.NoError(t, service.SetStaleDetail(seed.ID, true))
	require.NoError(t, service.SetUnavailableDetail(seed.ID, true))
	before, err := service.GetMetadata(seed.ProjectID, seed.ID)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"kept": "seed", "old": "remove"}, before)
	before["kept"] = "changed pre-update read"

	input := map[string]string{"kept": "new"}
	replaced, err := service.ReplaceMetadata(seed.ProjectID, seed.ID, input)
	require.NoError(t, err)
	assert.Equal(t, input, replaced)
	input["kept"] = "changed replacement input"
	replaced["kept"] = "changed replacement result"
	want := seed
	want.Metadata = map[string]string{"kept": "new"}
	want.UpdatedAt = now.UTC()
	live := service.List(seed.ProjectID, app.ListFilter{})
	require.Len(t, live, 1)
	assert.Equal(t, want, live[0].Server)
	live[0].Metadata["kept"] = "changed list result"

	now = now.Add(time.Minute)
	_, err = service.ReplaceMetadata(seed.ProjectID, seed.ID, want.Metadata)
	require.NoError(t, err)
	live = service.List(seed.ProjectID, app.ListFilter{})
	require.Len(t, live, 1)
	assert.Equal(t, want, live[0].Server)
	_, err = service.Get(seed.ProjectID, seed.ID)
	assert.ErrorIs(t, err, app.ErrDetailUnavailable)
	stale, err := service.Get(seed.ProjectID, seed.ID)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"kept": "seed", "old": "remove"}, stale.Metadata)
	stale.Metadata["kept"] = "changed stale result"
	staleAgain, err := service.Get(seed.ProjectID, seed.ID)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"kept": "seed", "old": "remove"}, staleAgain.Metadata)
	service.Reset()
	reset, err := service.GetMetadata(seed.ProjectID, seed.ID)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"kept": "seed", "old": "remove"}, reset)
}

func TestServerMetadataScopeQuotaAndWriteState(t *testing.T) {
	seed := domain.Server{ID: "server", ProjectID: "project", Stopped: true}
	service := app.New(seed, time.Now, nil)
	baseline, err := service.GetMetadata(seed.ProjectID, seed.ID)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{}, baseline)
	_, err = service.GetMetadata("foreign", seed.ID)
	assert.ErrorIs(t, err, app.ErrServerNotFound)
	_, err = service.ReplaceMetadata(seed.ProjectID, "unknown", nil)
	assert.ErrorIs(t, err, app.ErrServerNotFound)

	values := make(map[string]string, 129)
	for i := range 128 {
		values[strconv.Itoa(i)] = "value"
	}
	replaced, err := service.ReplaceMetadata(seed.ProjectID, seed.ID, values)
	require.NoError(t, err)
	assert.Equal(t, values, replaced)
	values["excess"] = "value"
	_, err = service.ReplaceMetadata(seed.ProjectID, seed.ID, values)
	assert.ErrorIs(t, err, app.ErrServerMetadataQuota)
	require.NoError(t, service.SetFault(seed.ID, true))
	_, err = service.ReplaceMetadata(seed.ProjectID, seed.ID, nil)
	assert.ErrorIs(t, err, app.ErrServerMetadataState)
	metadata, err := service.GetMetadata(seed.ProjectID, seed.ID)
	require.NoError(t, err)
	assert.Equal(t, replaced, metadata)
}
