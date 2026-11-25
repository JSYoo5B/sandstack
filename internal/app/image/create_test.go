package image_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	app "github.com/JSYoo5B/sandstack/internal/app/image"
	domain "github.com/JSYoo5B/sandstack/internal/domain/image"
)

func TestCreatedImageRecordsPageAndResetIndependentlyOfSeedData(t *testing.T) {
	const data = "fixture image bytes"
	start := time.Date(2026, time.November, 25, 1, 2, 3, 0, time.UTC)
	now := start.Add(time.Minute).In(time.FixedZone("fixture", 3600))
	seed := domain.Image{
		ID: "00000010-0000-4000-8000-000000000000", ProjectID: "owner", CreatedAt: start, UpdatedAt: start,
	}
	ids := []string{
		seed.ID, "00000001-0000-4000-8000-000000000000",
		"00000009-0000-4000-8000-000000000000", "00000003-0000-4000-8000-000000000000",
	}
	service := app.New(seed, func() time.Time { return now }, data, func() string {
		id := ids[0]
		ids = ids[1:]
		return id
	})
	baseline, err := service.Get(seed.ProjectID, seed.ID)
	require.NoError(t, err)
	require.NoError(t, service.SetUnavailableDownload(seed.ID, true))
	_, err = service.Create("foreign", app.CreateOptions{})
	assert.ErrorIs(t, err, app.ErrImageNotFound)
	first, err := service.Create(seed.ProjectID, app.CreateOptions{
		Name: "queued", ContainerFormat: domain.ContainerFormatBare, DiskFormat: domain.DiskFormatQCOW2,
	})
	require.NoError(t, err)
	assert.NotEqual(t, seed.ID, first.ID)
	assert.Equal(t, now.UTC(), first.CreatedAt)
	assert.Equal(t, first.CreatedAt, first.UpdatedAt)
	assert.Equal(t, "queued", first.Status())
	assert.Equal(t, "bare", first.ContainerFormat)
	assert.Equal(t, "qcow2", first.DiskFormat)
	second, err := service.Create(seed.ProjectID, app.CreateOptions{Name: "marker"})
	require.NoError(t, err)
	now = now.Add(time.Minute)
	third, err := service.Create(seed.ProjectID, app.CreateOptions{Name: "queued"})
	require.NoError(t, err)
	assert.Empty(t, third.ContainerFormat)
	assert.Empty(t, third.DiskFormat)

	page, err := service.ListPage(seed.ProjectID, app.ListFilter{}, nil, 2)
	require.NoError(t, err)
	assert.Equal(t, app.ImagePage{Images: []domain.Image{third, second}, NextMarker: second.ID}, page)
	page, err = service.ListPage(seed.ProjectID, app.ListFilter{}, &second.ID, 2)
	require.NoError(t, err)
	assert.Equal(t, app.ImagePage{Images: []domain.Image{first, baseline}, NextMarker: seed.ID}, page)
	filter := app.ListFilter{Name: &first.Name}
	page, err = service.ListPage(seed.ProjectID, filter, &second.ID, 2)
	require.NoError(t, err)
	assert.Equal(t, app.ImagePage{Images: []domain.Image{first}}, page)
	read, err := service.Get(seed.ProjectID, first.ID)
	require.NoError(t, err)
	assert.Equal(t, first, read)
	name := "renamed"
	revised, err := service.Update(seed.ProjectID, first.ID, app.ImageUpdate{Name: &name})
	require.NoError(t, err)
	assert.Equal(t, name, revised.Name)
	assert.Equal(t, "queued", revised.Status())

	assert.ErrorIs(t, service.SetDeactivated(first.ID, true), app.ErrImageNotFound)
	assert.ErrorIs(t, service.SetUnavailableDownload(first.ID, true), app.ErrImageNotFound)
	size, err := service.CheckDownload(seed.ProjectID, first.ID)
	require.NoError(t, err)
	assert.Zero(t, size)
	download, checksum, err := service.Download(seed.ProjectID, first.ID)
	require.NoError(t, err)
	assert.Empty(t, download)
	assert.Empty(t, checksum)
	_, _, err = service.Download(seed.ProjectID, seed.ID)
	assert.ErrorIs(t, err, app.ErrDownloadUnavailable)

	service.Reset()
	assert.Equal(t, []domain.Image{baseline}, service.List(seed.ProjectID, app.ListFilter{}))
	_, err = service.Get(seed.ProjectID, first.ID)
	assert.ErrorIs(t, err, app.ErrImageNotFound)
	_, err = service.ListPage(seed.ProjectID, filter, &second.ID, 1)
	assert.ErrorIs(t, err, app.ErrImageNotFound)
	download, _, err = service.Download(seed.ProjectID, seed.ID)
	require.NoError(t, err)
	assert.Equal(t, data, download)
}

func TestCreateImageBoundsIDCollisions(t *testing.T) {
	seed := domain.Image{ID: "00000010-0000-4000-8000-000000000000", ProjectID: "owner"}
	attempts := 0
	service := app.New(seed, time.Now, "", func() string {
		attempts++
		return seed.ID
	})

	_, err := service.Create(seed.ProjectID, app.CreateOptions{})
	assert.ErrorIs(t, err, app.ErrImageIDGeneration)
	assert.Equal(t, 8, attempts)
	assert.Equal(t, []domain.Image{seed}, service.List(seed.ProjectID, app.ListFilter{}))
}
