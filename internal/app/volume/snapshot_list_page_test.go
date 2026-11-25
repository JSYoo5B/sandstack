package volume_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	app "github.com/JSYoo5B/sandstack/internal/app/volume"
	domain "github.com/JSYoo5B/sandstack/internal/domain/volume"
)

func TestSnapshotPagesFilterEmptyNamesAndUseScopedMarkers(t *testing.T) {
	seed := domain.Volume{ID: fixtureID, ProjectID: "project", State: domain.StateAvailable}
	ids := []string{
		"00000003-0000-4000-8000-000000000000",
		"00000002-0000-4000-8000-000000000000",
		"00000001-0000-4000-8000-000000000000",
	}
	service := app.New(seed, func() time.Time { return time.Time{} }, func() string {
		id := ids[0]
		ids = ids[1:]
		return id
	})
	create := func() domain.Snapshot {
		t.Helper()
		snapshot, err := service.CreateSnapshot(seed.ProjectID, seed.ID, "", "")
		require.NoError(t, err)
		return snapshot
	}
	setEmptyName := func(snapshot domain.Snapshot) domain.Snapshot {
		t.Helper()
		name := domain.NullableText{Valid: true}
		updated, _, err := service.UpdateSnapshot(seed.ProjectID, snapshot.ID, app.TextUpdate{Name: &name})
		require.NoError(t, err)
		return updated
	}
	last := setEmptyName(create())
	nameless := create()
	first := setEmptyName(create())
	status, empty := domain.StateCreating, ""
	filter := app.ListFilter{Name: &empty, Status: &status}

	page, err := service.ListSnapshotsPage(seed.ProjectID, filter, nil, 2)
	require.NoError(t, err)
	assert.Equal(t, app.SnapshotPage{Snapshots: []domain.Snapshot{first, last}, NextMarker: last.ID}, page)
	page, err = service.ListSnapshotsPage(seed.ProjectID, filter, &nameless.ID, 2)
	require.NoError(t, err)
	assert.Equal(t, app.SnapshotPage{Snapshots: []domain.Snapshot{last}}, page)
	page, err = service.ListSnapshotsPage(seed.ProjectID, filter, &last.ID, 2)
	require.NoError(t, err)
	assert.Equal(t, app.SnapshotPage{Snapshots: []domain.Snapshot{}}, page)
	page, err = service.ListSnapshotsPage(seed.ProjectID, filter, &nameless.ID, 0)
	require.NoError(t, err)
	assert.Equal(t, app.SnapshotPage{Snapshots: []domain.Snapshot{}}, page)
	assert.Equal(t, []domain.Snapshot{first, nameless, last}, service.ListSnapshots(seed.ProjectID))

	unknown := "missing"
	for _, marker := range []*string{&empty, &unknown, &seed.ID} {
		_, err := service.ListSnapshotsPage(seed.ProjectID, filter, marker, 0)
		assert.ErrorIs(t, err, app.ErrSnapshotNotFound)
	}
	_, err = service.ListSnapshotsPage("other-project", filter, &last.ID, 1)
	assert.ErrorIs(t, err, app.ErrSnapshotNotFound)

	require.NoError(t, service.FinishSnapshotCreation(last.ID, false))
	page, err = service.ListSnapshotsPage(seed.ProjectID, filter, nil, 2)
	require.NoError(t, err)
	assert.Equal(t, app.SnapshotPage{Snapshots: []domain.Snapshot{first}}, page)
}
