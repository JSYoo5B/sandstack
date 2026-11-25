package volume_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	app "github.com/JSYoo5B/sandstack/internal/app/volume"
	domain "github.com/JSYoo5B/sandstack/internal/domain/volume"
)

func TestVolumePageUsesScopedMarkersWithoutConsumingDetailFailures(t *testing.T) {
	seed := domain.Volume{ID: fixtureID, ProjectID: "project", State: domain.StateAvailable}
	ids := []string{createdID, "00000020-0000-4000-8000-000000000000"}
	service := app.New(seed, func() time.Time { return time.Time{} }, func() string {
		id := ids[0]
		ids = ids[1:]
		return id
	})
	first, err := service.Create(seed.ProjectID, "filtered", "", 1)
	require.NoError(t, err)
	last, err := service.Create(seed.ProjectID, "filtered", "", 1)
	require.NoError(t, err)
	require.NoError(t, service.SetUnavailableDetail(seed.ID, true))
	require.NoError(t, service.SetUnavailableDetail(last.ID, true))
	status := domain.StateCreating
	filter := app.ListFilter{Status: &status}

	page, err := service.ListPage(seed.ProjectID, filter, nil, 1)
	require.NoError(t, err)
	assert.Equal(t, app.VolumePage{Volumes: []domain.Volume{first}, NextMarker: first.ID}, page)
	page, err = service.ListPage(seed.ProjectID, filter, &seed.ID, 1)
	require.NoError(t, err)
	assert.Equal(t, app.VolumePage{Volumes: []domain.Volume{last}, NextMarker: last.ID}, page)
	page, err = service.ListPage(seed.ProjectID, filter, &last.ID, 1)
	require.NoError(t, err)
	assert.Equal(t, app.VolumePage{Volumes: []domain.Volume{}}, page)
	page, err = service.ListPage(seed.ProjectID, filter, &first.ID, 2)
	require.NoError(t, err)
	assert.Equal(t, app.VolumePage{Volumes: []domain.Volume{last}}, page)
	page, err = service.ListPage(seed.ProjectID, filter, &seed.ID, 0)
	require.NoError(t, err)
	assert.Equal(t, app.VolumePage{Volumes: []domain.Volume{}}, page)

	empty, unknown := "", "missing"
	for _, marker := range []*string{&empty, &unknown} {
		_, err := service.ListPage(seed.ProjectID, filter, marker, 0)
		assert.ErrorIs(t, err, app.ErrVolumeNotFound)
	}
	_, err = service.ListPage("foreign", filter, &seed.ID, 1)
	assert.ErrorIs(t, err, app.ErrVolumeNotFound)
	assert.Equal(t, []domain.Volume{first, seed, last}, service.List(seed.ProjectID, app.ListFilter{}))
	for _, id := range []string{seed.ID, last.ID} {
		_, err := service.Get(seed.ProjectID, id)
		assert.ErrorIs(t, err, app.ErrDetailUnavailable)
	}
}
