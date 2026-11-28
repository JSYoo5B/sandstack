package volume_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	app "github.com/JSYoo5B/sandstack/internal/app/volume"
	domain "github.com/JSYoo5B/sandstack/internal/domain/volume"
)

func TestVolumeTypePaginationRequiresAConcreteScopedMarker(t *testing.T) {
	seed := domain.Volume{ID: fixtureID, ProjectID: "project"}
	service := app.New(seed, time.Now, nil)
	standard, err := service.GetVolumeType(seed.ProjectID, "default")
	require.NoError(t, err)
	performance, err := service.GetVolumeType(seed.ProjectID, performanceTypeID)
	require.NoError(t, err)
	empty := app.VolumeTypePage{Types: []domain.VolumeType{}}

	page, err := service.ListVolumeTypesPage(seed.ProjectID, nil, 1)
	require.NoError(t, err)
	assert.Equal(t, app.VolumeTypePage{Types: []domain.VolumeType{standard}, NextMarker: standard.ID}, page)
	page, err = service.ListVolumeTypesPage(seed.ProjectID, &standard.ID, 1)
	require.NoError(t, err)
	assert.Equal(t, app.VolumeTypePage{Types: []domain.VolumeType{performance}, NextMarker: performance.ID}, page)
	page, err = service.ListVolumeTypesPage(seed.ProjectID, &performance.ID, 1)
	require.NoError(t, err)
	assert.Equal(t, empty, page)
	page, err = service.ListVolumeTypesPage(seed.ProjectID, nil, 2)
	require.NoError(t, err)
	assert.Equal(t, app.VolumeTypePage{
		Types: []domain.VolumeType{standard, performance}, NextMarker: performance.ID,
	}, page)
	page, err = service.ListVolumeTypesPage(seed.ProjectID, nil, 3)
	require.NoError(t, err)
	assert.Equal(t, app.VolumeTypePage{Types: []domain.VolumeType{standard, performance}}, page)
	page, err = service.ListVolumeTypesPage(seed.ProjectID, nil, 0)
	require.NoError(t, err)
	assert.Equal(t, empty, page)

	for _, marker := range []string{"", "default", standard.Name, performance.Name, "missing"} {
		_, err := service.ListVolumeTypesPage(seed.ProjectID, &marker, 0)
		assert.ErrorIs(t, err, app.ErrVolumeTypeNotFound)
	}
	_, err = service.ListVolumeTypesPage("foreign", &standard.ID, 1)
	assert.ErrorIs(t, err, app.ErrVolumeTypeNotFound)
	page, err = service.ListVolumeTypesPage("foreign", nil, 1)
	require.NoError(t, err)
	assert.Equal(t, empty, page)
}
