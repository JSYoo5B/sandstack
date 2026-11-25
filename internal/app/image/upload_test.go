package image_test

import (
	"crypto/md5"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	app "github.com/JSYoo5B/sandstack/internal/app/image"
	domain "github.com/JSYoo5B/sandstack/internal/domain/image"
)

func TestConcurrentImageUploadsActivateExactlyOnePayload(t *testing.T) {
	const id = "00000001-0000-4000-8000-000000000000"
	seed := domain.Image{ID: "seed", ProjectID: "owner"}
	start := time.Date(2026, time.November, 25, 1, 2, 3, 0, time.UTC)
	now := start.In(time.FixedZone("fixture", 3600))
	service := app.New(seed, func() time.Time { return now }, "seed bytes", func() string { return id })
	options := app.CreateOptions{
		Name: "upload", ContainerFormat: domain.ContainerFormatBare, DiskFormat: domain.DiskFormatQCOW2,
	}
	queued, err := service.Create(seed.ProjectID, options)
	require.NoError(t, err)
	require.NoError(t, service.CheckUpload(seed.ProjectID, queued.ID))
	assert.ErrorIs(t, service.Upload("foreign", queued.ID, "discard me"), app.ErrImageNotFound)
	assert.ErrorIs(t, service.CheckUpload(seed.ProjectID, seed.ID), app.ErrImageUploadState)
	now = now.Add(time.Minute)

	const attempts = 8
	type result struct {
		payload string
		err     error
	}
	begin := make(chan struct{})
	results := make(chan result, attempts)
	for i := range attempts {
		go func() {
			<-begin
			payload := fmt.Sprintf("image bytes %d", i)
			results <- result{payload: payload, err: service.Upload(seed.ProjectID, queued.ID, payload)}
		}()
	}
	close(begin)
	successes := 0
	var uploaded string
	for range attempts {
		response := <-results
		if response.err == nil {
			successes++
			uploaded = response.payload
			continue
		}
		assert.ErrorIs(t, response.err, app.ErrImageUploadState)
	}
	require.Equal(t, 1, successes)

	active, err := service.Get(seed.ProjectID, queued.ID)
	require.NoError(t, err)
	want := queued
	want.Queued = false
	want.Size = int64(len(uploaded))
	want.Checksum = fmt.Sprintf("%x", md5.Sum([]byte(uploaded)))
	want.UpdatedAt = now.UTC()
	assert.Equal(t, want, active)
	data, checksum, err := service.Download(seed.ProjectID, queued.ID)
	require.NoError(t, err)
	assert.Equal(t, uploaded, data)
	assert.Equal(t, want.Checksum, checksum)
	assert.ErrorIs(t, service.CheckUpload(seed.ProjectID, queued.ID), app.ErrImageUploadState)

	require.NoError(t, service.Delete(seed.ProjectID, queued.ID))
	_, _, err = service.Download(seed.ProjectID, queued.ID)
	assert.ErrorIs(t, err, app.ErrImageNotFound)
	service.Reset()
	queued, err = service.Create(seed.ProjectID, options)
	require.NoError(t, err)
	data, checksum, err = service.Download(seed.ProjectID, queued.ID)
	require.NoError(t, err)
	assert.Empty(t, data)
	assert.Empty(t, checksum)
	data, _, err = service.Download(seed.ProjectID, seed.ID)
	require.NoError(t, err)
	assert.Equal(t, "seed bytes", data)
}

func TestImageUploadRequiresFormatsAndChecksumsEmptyPayload(t *testing.T) {
	seed := domain.Image{ID: "seed", ProjectID: "owner"}
	ids := []string{"00000001-0000-4000-8000-000000000000", "00000002-0000-4000-8000-000000000000"}
	service := app.New(seed, time.Now, "", func() string {
		id := ids[0]
		ids = ids[1:]
		return id
	})
	noFormats, err := service.Create(seed.ProjectID, app.CreateOptions{})
	require.NoError(t, err)
	assert.ErrorIs(t, service.CheckUpload(seed.ProjectID, noFormats.ID), app.ErrImageUploadFormats)
	assert.ErrorIs(t, service.Upload(seed.ProjectID, noFormats.ID, "discard me"), app.ErrImageUploadFormats)
	unchanged, err := service.Get(seed.ProjectID, noFormats.ID)
	require.NoError(t, err)
	assert.Equal(t, noFormats, unchanged)
	formatted, err := service.Create(seed.ProjectID, app.CreateOptions{
		ContainerFormat: domain.ContainerFormatBare, DiskFormat: domain.DiskFormatQCOW2,
	})
	require.NoError(t, err)

	require.NoError(t, service.Upload(seed.ProjectID, formatted.ID, ""))
	active, err := service.Get(seed.ProjectID, formatted.ID)
	require.NoError(t, err)
	assert.Equal(t, "active", active.Status())
	assert.Zero(t, active.Size)
	assert.Equal(t, "d41d8cd98f00b204e9800998ecf8427e", active.Checksum)
}
