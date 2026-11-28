package volume_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	app "github.com/JSYoo5B/sandstack/internal/app/volume"
	domain "github.com/JSYoo5B/sandstack/internal/domain/volume"
)

const standardTypeID = "3190e22a-9606-4cf7-b9bd-4eb5af3dc601"

// https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/volume/api.py#L1883-L2004
func TestRetypeAcceptsAvailableAndInUseVolumesWithSnapshots(t *testing.T) {
	start := time.Date(2026, time.November, 28, 1, 2, 3, 0, time.UTC)
	now := start
	seed := domain.Volume{
		ID: fixtureID, ProjectID: "project", Size: 1, State: domain.StateInUse, UpdatedAt: start,
		Attachment: domain.Attachment{ID: "attachment", VolumeID: fixtureID, ServerID: "server"},
	}
	id := 64
	service := app.New(seed, func() time.Time { return now }, func() string {
		id++
		return fmt.Sprintf("%08x-0000-4000-8000-000000000000", id)
	})
	snapshot, err := service.CreateSnapshot(seed.ProjectID, seed.ID, app.SnapshotCreateOptions{Force: true})
	require.NoError(t, err)
	require.NoError(t, service.FinishSnapshotCreation(snapshot.ID, false))

	now = start.Add(time.Minute)
	foreignErr := service.Retype("foreign", seed.ID, "performance")
	missingErr := service.Retype(seed.ProjectID, "missing", "performance")
	unknownTypeErr := service.Retype(seed.ProjectID, seed.ID, "missing")
	aliasErr := service.Retype(seed.ProjectID, seed.ID, "default")
	sameErr := service.Retype(seed.ProjectID, seed.ID, standardTypeID)
	unchanged, err := service.Get(seed.ProjectID, seed.ID)
	require.NoError(t, err)

	assert.ErrorIs(t, foreignErr, app.ErrVolumeNotFound)
	assert.ErrorIs(t, missingErr, app.ErrVolumeNotFound)
	assert.ErrorIs(t, unknownTypeErr, app.ErrVolumeTypeNotFound)
	assert.ErrorIs(t, aliasErr, app.ErrVolumeTypeNotFound)
	assert.ErrorIs(t, sameErr, app.ErrVolumeTypeUnchanged)
	assert.Equal(t, seed, unchanged)

	require.NoError(t, service.Retype(seed.ProjectID, seed.ID, "performance"))
	retyped, err := service.Get(seed.ProjectID, seed.ID)
	require.NoError(t, err)
	clone, err := service.CreateFromSnapshot(seed.ProjectID, snapshot.ID, "", "", 0)
	require.NoError(t, err)
	sameAgainErr := service.Retype(seed.ProjectID, seed.ID, performanceTypeID)

	want := seed
	want.TypeID = performanceTypeID
	want.UpdatedAt = now
	assert.Equal(t, want, retyped)
	assert.Equal(t, "performance", app.VolumeTypeName(retyped.TypeID))
	assert.Equal(t, performanceTypeID, clone.TypeID)
	assert.ErrorIs(t, sameAgainErr, app.ErrVolumeTypeUnchanged)

	require.NoError(t, service.Retype(seed.ProjectID, seed.ID, "standard"))
	restored, err := service.Get(seed.ProjectID, seed.ID)
	require.NoError(t, err)
	assert.Equal(t, standardTypeID, restored.TypeID)
	assert.Equal(t, domain.StateInUse, restored.State)

	service.Reset()
	reset, err := service.Get(seed.ProjectID, seed.ID)
	require.NoError(t, err)
	assert.Equal(t, seed, reset)
}

func TestRetypeRejectsTransitionalStates(t *testing.T) {
	seed := domain.Volume{ID: fixtureID, ProjectID: "project", Size: 1}
	service := app.New(seed, time.Now, func() string { return "00000080-0000-4000-8000-000000000000" })
	creating, err := service.Create(seed.ProjectID, "", "", 1)
	require.NoError(t, err)
	require.NoError(t, service.StartExtension(seed.ProjectID, seed.ID, 2))

	creatingErr := service.Retype(seed.ProjectID, creating.ID, "performance")
	extendingErr := service.Retype(seed.ProjectID, seed.ID, "performance")

	assert.ErrorIs(t, creatingErr, app.ErrVolumeStateConflict)
	assert.ErrorIs(t, extendingErr, app.ErrVolumeStateConflict)
}

// https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/volume/flows/api/create_volume.py#L397-L430
func TestCreateVolumeSelectsOrInheritsVolumeType(t *testing.T) {
	seed := domain.Volume{ID: fixtureID, ProjectID: "project", Size: 1, State: domain.StateAvailable}
	id := 96
	service := app.New(seed, time.Now, func() string {
		id++
		return fmt.Sprintf("%08x-0000-4000-8000-000000000000", id)
	})

	byName, err := service.CreateVolume(seed.ProjectID, app.CreateRequest{VolumeType: "performance", Size: 1})
	require.NoError(t, err)
	require.NoError(t, service.CompleteCreation(byName.ID))
	inherited, err := service.CreateVolume(seed.ProjectID, app.CreateRequest{SourceVolumeID: byName.ID})
	require.NoError(t, err)
	explicit, err := service.CreateVolume(seed.ProjectID, app.CreateRequest{
		SourceVolumeID: byName.ID, VolumeType: standardTypeID,
	})
	require.NoError(t, err)
	defaulted, err := service.CreateVolume(seed.ProjectID, app.CreateRequest{Size: 1})
	require.NoError(t, err)
	_, unknownErr := service.CreateVolume(seed.ProjectID, app.CreateRequest{VolumeType: "missing", Size: 1})

	assert.Equal(t, performanceTypeID, byName.TypeID)
	assert.Equal(t, performanceTypeID, inherited.TypeID)
	assert.Equal(t, standardTypeID, explicit.TypeID)
	assert.Equal(t, "standard", app.VolumeTypeName(defaulted.TypeID))
	assert.ErrorIs(t, unknownErr, app.ErrVolumeTypeNotFound)
}
