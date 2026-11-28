package volume_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	app "github.com/JSYoo5B/sandstack/internal/app/volume"
	domain "github.com/JSYoo5B/sandstack/internal/domain/volume"
)

func TestVolumeTypePaginationRequiresAConcreteMarker(t *testing.T) {
	seed := domain.Volume{ID: fixtureID, ProjectID: "project"}
	service := app.New(seed, time.Now, nil)
	standard, err := service.GetVolumeType("default")
	require.NoError(t, err)
	performance, err := service.GetVolumeType(performanceTypeID)
	require.NoError(t, err)
	empty := app.VolumeTypePage{Types: []domain.VolumeType{}}

	page, err := service.ListVolumeTypesPage(nil, 1)
	require.NoError(t, err)
	assert.Equal(t, app.VolumeTypePage{Types: []domain.VolumeType{standard}, NextMarker: standard.ID}, page)
	page, err = service.ListVolumeTypesPage(&standard.ID, 1)
	require.NoError(t, err)
	assert.Equal(t, app.VolumeTypePage{Types: []domain.VolumeType{performance}, NextMarker: performance.ID}, page)
	page, err = service.ListVolumeTypesPage(&performance.ID, 1)
	require.NoError(t, err)
	assert.Equal(t, empty, page)
	page, err = service.ListVolumeTypesPage(nil, 2)
	require.NoError(t, err)
	assert.Equal(t, app.VolumeTypePage{
		Types: []domain.VolumeType{standard, performance}, NextMarker: performance.ID,
	}, page)
	page, err = service.ListVolumeTypesPage(nil, 3)
	require.NoError(t, err)
	assert.Equal(t, app.VolumeTypePage{Types: []domain.VolumeType{standard, performance}}, page)
	page, err = service.ListVolumeTypesPage(nil, 0)
	require.NoError(t, err)
	assert.Equal(t, empty, page)

	for _, marker := range []string{"", "default", standard.Name, performance.Name, "missing"} {
		_, err := service.ListVolumeTypesPage(&marker, 0)
		assert.ErrorIs(t, err, app.ErrVolumeTypeNotFound)
	}
}
