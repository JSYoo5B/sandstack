package image_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	app "github.com/JSYoo5B/sandstack/internal/app/image"
	domain "github.com/JSYoo5B/sandstack/internal/domain/image"
)

func TestDeletedImagesKeepIDsReservedUntilReset(t *testing.T) {
	const seedID = "00000010-0000-4000-8000-000000000000"
	const queuedID = "00000011-0000-4000-8000-000000000000"
	const nextID = "00000012-0000-4000-8000-000000000000"
	const data = "fixture image bytes"
	seed := domain.Image{ID: seedID, ProjectID: "owner", Name: "seed"}
	ids := []string{queuedID, queuedID, seedID, nextID, queuedID}
	service := app.New(seed, time.Now, data, func() string {
		id := ids[0]
		ids = ids[1:]
		return id
	})
	baseline, err := service.Get(seed.ProjectID, seed.ID)
	require.NoError(t, err)
	queued, err := service.Create(seed.ProjectID, app.CreateOptions{Name: "queued"})
	require.NoError(t, err)
	require.Equal(t, queuedID, queued.ID)
	assert.ErrorIs(t, service.Delete("foreign", queued.ID), app.ErrImageNotFound)
	_, err = service.Get(seed.ProjectID, queued.ID)
	require.NoError(t, err)
	require.NoError(t, service.SetUnavailableDownload(seed.ID, true))
	require.NoError(t, service.SetDeactivated(seed.ID, true))

	require.NoError(t, service.Delete(seed.ProjectID, queued.ID))
	assert.ErrorIs(t, service.Delete(seed.ProjectID, queued.ID), app.ErrImageNotFound)
	require.NoError(t, service.Delete(seed.ProjectID, seed.ID))
	assert.ErrorIs(t, service.SetDeactivated(seed.ID, true), app.ErrImageNotFound)
	assert.ErrorIs(t, service.SetUnavailableDownload(seed.ID, true), app.ErrImageNotFound)
	_, _, err = service.Download(seed.ProjectID, seed.ID)
	assert.ErrorIs(t, err, app.ErrImageNotFound)
	assert.Empty(t, service.List(seed.ProjectID, app.ListFilter{}))

	newImage, err := service.Create(seed.ProjectID, app.CreateOptions{Name: "next"})
	require.NoError(t, err)
	assert.Equal(t, nextID, newImage.ID)
	for _, id := range []string{seed.ID, queued.ID} {
		_, err := service.ListPage(seed.ProjectID, app.ListFilter{}, &id, 1)
		assert.ErrorIs(t, err, app.ErrImageNotFound)
	}

	service.Reset()
	assert.Equal(t, []domain.Image{baseline}, service.List(seed.ProjectID, app.ListFilter{}))
	bytes, checksum, err := service.Download(seed.ProjectID, seed.ID)
	require.NoError(t, err)
	assert.Equal(t, data, bytes)
	assert.Equal(t, baseline.Checksum, checksum)
	reused, err := service.Create(seed.ProjectID, app.CreateOptions{})
	require.NoError(t, err)
	assert.Equal(t, queuedID, reused.ID)
}
