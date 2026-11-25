package image_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	app "github.com/JSYoo5B/sandstack/internal/app/image"
	domain "github.com/JSYoo5B/sandstack/internal/domain/image"
)

func TestImageListPageResolvesMarkerBeforeFilters(t *testing.T) {
	seed := domain.Image{ID: "image", ProjectID: "owner", Name: "fixture", Tags: []string{"test"}}
	service := app.New(seed, time.Now, "", nil)
	active := "active"
	filter := app.ListFilter{Name: &seed.Name, Status: &active, Tags: []string{"test"}}
	empty := app.ImagePage{Images: []domain.Image{}}

	page, err := service.ListPage(seed.ProjectID, filter, nil, 1)
	require.NoError(t, err)
	assert.Equal(t, app.ImagePage{Images: []domain.Image{seed}, NextMarker: seed.ID}, page)
	page.Images[0].Tags[0] = "changed caller tag"
	page, err = service.ListPage(seed.ProjectID, filter, nil, 2)
	require.NoError(t, err)
	assert.Equal(t, app.ImagePage{Images: []domain.Image{seed}}, page)
	page, err = service.ListPage(seed.ProjectID, filter, nil, 0)
	require.NoError(t, err)
	assert.Equal(t, empty, page)

	filter.Tags = []string{"does not match"}
	page, err = service.ListPage(seed.ProjectID, filter, &seed.ID, 1)
	require.NoError(t, err)
	assert.Equal(t, empty, page)
	page, err = service.ListPage("foreign", filter, nil, 1)
	require.NoError(t, err)
	assert.Equal(t, empty, page)
	_, err = service.ListPage("foreign", filter, &seed.ID, 0)
	assert.ErrorIs(t, err, app.ErrImageMarkerForbidden)
	for _, marker := range []string{"", "unknown"} {
		_, err := service.ListPage(seed.ProjectID, filter, &marker, 0)
		assert.ErrorIs(t, err, app.ErrImageNotFound)
	}
}
