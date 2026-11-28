package volume_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	app "github.com/JSYoo5B/sandstack/internal/app/volume"
	domain "github.com/JSYoo5B/sandstack/internal/domain/volume"
)

func TestVolumeMetadataReplacementPreservesAttachmentAndDetailFailure(t *testing.T) {
	now := time.Date(2026, time.November, 25, 12, 0, 0, 0, time.UTC)
	seed := domain.Volume{
		ID: fixtureID, ProjectID: "project", State: domain.StateInUse, UpdatedAt: now.Add(-time.Hour),
		Attachment: domain.Attachment{ID: "attachment", VolumeID: fixtureID, ServerID: "server"},
		Metadata:   map[string]string{"baseline": "seed"},
	}
	service := app.New(seed, func() time.Time { return now }, func() string { return createdID })
	seed.Metadata["baseline"] = "mutated seed"
	baseline, err := service.Get(seed.ProjectID, seed.ID)
	require.NoError(t, err)
	require.Equal(t, map[string]string{"baseline": "seed"}, baseline.Metadata)
	require.NoError(t, service.SetUnavailableDetail(seed.ID, true))
	other, err := service.Create(seed.ProjectID, seed.UserID, "other volume", "", 1)
	require.NoError(t, err)

	input := map[string]string{"stage": "ready"}
	replaced, err := service.ReplaceVolumeMetadata(seed.ProjectID, seed.ID, input)
	require.NoError(t, err)
	assert.Equal(t, input, replaced)
	input["stage"] = "mutated input"
	replaced["stage"] = "mutated replacement"
	metadata, err := service.GetVolumeMetadata(seed.ProjectID, seed.ID)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"stage": "ready"}, metadata)
	metadata["stage"] = "mutated metadata read"
	want := baseline
	want.Metadata = map[string]string{"stage": "ready"}

	updated, err := service.Update(seed.ProjectID, seed.ID, app.TextUpdate{})
	require.NoError(t, err)
	assert.Equal(t, want, updated)
	updated.Metadata["stage"] = "mutated update"
	listed := service.List(seed.ProjectID, app.ListFilter{})
	require.Equal(t, []domain.Volume{other, want}, listed)
	listed[1].Metadata["stage"] = "mutated list"
	page, err := service.ListPage(seed.ProjectID, app.ListFilter{}, nil, 1)
	require.NoError(t, err)
	require.Equal(t, []domain.Volume{other}, page.Volumes)
	assert.Equal(t, len(page.Volumes), cap(page.Volumes), "the page cannot expose records after its last item")
	page, err = service.ListPage(seed.ProjectID, app.ListFilter{}, &other.ID, 1)
	require.NoError(t, err)
	require.Equal(t, []domain.Volume{want}, page.Volumes)
	page.Volumes[0].Metadata["stage"] = "mutated page"

	_, err = service.Get(seed.ProjectID, seed.ID)
	assert.ErrorIs(t, err, app.ErrDetailUnavailable)
	read, err := service.Get(seed.ProjectID, seed.ID)
	require.NoError(t, err)
	assert.Equal(t, want, read)
	read.Metadata["stage"] = "mutated read"
	metadata, err = service.GetVolumeMetadata(seed.ProjectID, seed.ID)
	require.NoError(t, err)
	assert.Equal(t, want.Metadata, metadata)

	_, err = service.GetVolumeMetadata("foreign", seed.ID)
	assert.ErrorIs(t, err, app.ErrVolumeNotFound)
	_, err = service.ReplaceVolumeMetadata(seed.ProjectID, "missing", nil)
	assert.ErrorIs(t, err, app.ErrVolumeNotFound)
	cleared, err := service.ReplaceVolumeMetadata(seed.ProjectID, seed.ID, map[string]string{})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{}, cleared)
	service.Reset()
	reset, err := service.Get(seed.ProjectID, seed.ID)
	require.NoError(t, err)
	assert.Equal(t, baseline, reset)
}
