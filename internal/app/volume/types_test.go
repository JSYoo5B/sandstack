package volume_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	app "github.com/JSYoo5B/sandstack/internal/app/volume"
	domain "github.com/JSYoo5B/sandstack/internal/domain/volume"
)

const performanceTypeID = "6f0c2b8e-4d1a-4e7b-9c35-2a8f1d7e5b90"

func TestDefaultVolumeTypeAndIDShareProjectScope(t *testing.T) {
	seed := domain.Volume{ID: fixtureID, ProjectID: "project"}
	service := app.New(seed, time.Now, nil)

	defaultType, err := service.GetVolumeType(seed.ProjectID, "default")
	require.NoError(t, err)
	assert.Equal(t, domain.VolumeType{
		ID:          "3190e22a-9606-4cf7-b9bd-4eb5af3dc601",
		Name:        "standard",
		Description: "Standard SandStack volume type",
		Public:      true,
	}, defaultType)
	byID, err := service.GetVolumeType(seed.ProjectID, defaultType.ID)
	require.NoError(t, err)
	assert.Equal(t, defaultType, byID)

	for _, id := range []string{"default", defaultType.ID} {
		_, err := service.GetVolumeType("foreign", id)
		assert.ErrorIs(t, err, app.ErrVolumeTypeNotFound)
	}
	for _, id := range []string{"missing", "standard", "performance"} {
		_, err = service.GetVolumeType(seed.ProjectID, id)
		assert.ErrorIs(t, err, app.ErrVolumeTypeNotFound)
	}
}

func TestPerformanceVolumeTypeIsReadableByID(t *testing.T) {
	seed := domain.Volume{ID: fixtureID, ProjectID: "project"}
	service := app.New(seed, time.Now, nil)

	performance, err := service.GetVolumeType(seed.ProjectID, performanceTypeID)
	_, foreignErr := service.GetVolumeType("foreign", performanceTypeID)

	require.NoError(t, err)
	assert.Equal(t, domain.VolumeType{
		ID:          performanceTypeID,
		Name:        "performance",
		Description: "Performance SandStack volume type",
		Public:      true,
	}, performance)
	assert.ErrorIs(t, foreignErr, app.ErrVolumeTypeNotFound)
}
