package volume_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	app "github.com/JSYoo5B/sandstack/internal/app/volume"
	domain "github.com/JSYoo5B/sandstack/internal/domain/volume"
)

func TestInitialVolumeMetadataIsDetachedForBothSources(t *testing.T) {
	seed := domain.Volume{
		ID: fixtureID, ProjectID: "project", Size: 5, Metadata: map[string]string{"seed": "metadata"},
	}
	ids := []string{createdID, "00000003-0000-4000-8000-000000000000", "00000004-0000-4000-8000-000000000000"}
	service := app.New(seed, func() time.Time { return time.Time{} }, func() string {
		id := ids[0]
		ids = ids[1:]
		return id
	})
	snapshot, err := service.CreateSnapshot(seed.ProjectID, seed.ID, app.SnapshotCreateOptions{
		Metadata: map[string]string{"snapshot": "metadata"},
	})
	require.NoError(t, err)
	require.NoError(t, service.FinishSnapshotCreation(snapshot.ID, false))

	input := map[string]string{"stage": "blank"}
	blank, err := service.CreateWithMetadata(seed.ProjectID, seed.UserID, "blank", "", 2, input)
	require.NoError(t, err)
	assert.Equal(t, input, blank.Metadata)
	input["stage"] = "derived"
	derived, err := service.CreateFromSnapshotWithMetadata(seed.ProjectID, seed.UserID, snapshot.ID, "derived", "", 0, input)
	require.NoError(t, err)
	assert.Equal(t, input, derived.Metadata)
	assert.Equal(t, snapshot.ID, derived.SnapshotID)
	assert.Equal(t, snapshot.Size, derived.Size)
	input["stage"] = "mutated input"
	blank.Metadata["stage"] = "mutated blank response"
	derived.Metadata["stage"] = "mutated derived response"

	readBlank, err := service.GetVolumeMetadata(seed.ProjectID, blank.ID)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"stage": "blank"}, readBlank)
	readDerived, err := service.GetVolumeMetadata(seed.ProjectID, derived.ID)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"stage": "derived"}, readDerived)
	readSnapshot, err := service.GetSnapshot(seed.ProjectID, snapshot.ID)
	require.NoError(t, err)
	assert.Equal(t, snapshot.Metadata, readSnapshot.Metadata)
}
