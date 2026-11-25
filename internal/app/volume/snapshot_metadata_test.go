package volume_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	app "github.com/JSYoo5B/sandstack/internal/app/volume"
	domain "github.com/JSYoo5B/sandstack/internal/domain/volume"
)

func TestSnapshotMetadataReplacementDetachesMapsAndPreservesSnapshot(t *testing.T) {
	now := time.Date(2026, time.November, 25, 12, 0, 0, 0, time.UTC)
	seed := domain.Volume{ID: fixtureID, ProjectID: "project", Size: 5}
	service := app.New(seed, func() time.Time { return now }, func() string { return createdID })
	initial := map[string]string{"team/checkpoint": "initial"}
	created, err := service.CreateSnapshot(seed.ProjectID, seed.ID, app.SnapshotCreateOptions{
		Name: "checkpoint", Metadata: initial,
	})
	require.NoError(t, err)
	assert.Equal(t, initial, created.Metadata)
	initial["team/checkpoint"] = "mutated input"
	created.Metadata["caller"] = "not stored"
	now = now.Add(time.Minute)
	require.NoError(t, service.FinishSnapshotCreation(created.ID, false))
	before, err := service.GetSnapshot(seed.ProjectID, created.ID)
	require.NoError(t, err)
	require.Equal(t, map[string]string{"team/checkpoint": "initial"}, before.Metadata)

	input := map[string]string{"stage": "ready", "obsolete": "remove"}
	replaced, err := service.ReplaceSnapshotMetadata(seed.ProjectID, created.ID, input)
	require.NoError(t, err)
	assert.Equal(t, input, replaced)
	input["stage"] = "mutated input"
	replaced["stage"] = "mutated result"
	want := before
	want.Metadata = map[string]string{"stage": "ready", "obsolete": "remove"}
	read, err := service.GetSnapshot(seed.ProjectID, created.ID)
	require.NoError(t, err)
	assert.Equal(t, want, read)
	read.Metadata["stage"] = "mutated read"
	updated, _, err := service.UpdateSnapshot(seed.ProjectID, created.ID, app.TextUpdate{})
	require.NoError(t, err)
	assert.Equal(t, want, updated)
	updated.Metadata["stage"] = "mutated update"
	listed := service.ListSnapshots(seed.ProjectID)
	require.Equal(t, []domain.Snapshot{want}, listed)
	listed[0].Metadata["stage"] = "mutated list"
	page, err := service.ListSnapshotsPage(seed.ProjectID, app.ListFilter{}, nil, 1)
	require.NoError(t, err)
	require.Equal(t, []domain.Snapshot{want}, page.Snapshots)
	page.Snapshots[0].Metadata["stage"] = "mutated page"
	current, err := service.GetSnapshot(seed.ProjectID, created.ID)
	require.NoError(t, err)
	assert.Equal(t, want, current)

	_, err = service.ReplaceSnapshotMetadata("foreign", created.ID, nil)
	assert.ErrorIs(t, err, app.ErrSnapshotNotFound)
	_, err = service.ReplaceSnapshotMetadata(seed.ProjectID, "missing", nil)
	assert.ErrorIs(t, err, app.ErrSnapshotNotFound)
	replaced, err = service.ReplaceSnapshotMetadata(seed.ProjectID, created.ID, map[string]string{"stage": "done"})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"stage": "done"}, replaced)
	assert.Equal(t, want, current, "replacement leaves the last read detached")
	replaced, err = service.ReplaceSnapshotMetadata(seed.ProjectID, created.ID, map[string]string{})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{}, replaced)
	current, err = service.GetSnapshot(seed.ProjectID, created.ID)
	require.NoError(t, err)
	before.Metadata = map[string]string{}
	assert.Equal(t, before, current)
}
