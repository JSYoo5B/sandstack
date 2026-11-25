package image_test

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	app "github.com/JSYoo5B/sandstack/internal/app/image"
	domain "github.com/JSYoo5B/sandstack/internal/domain/image"
)

func TestDownloadUsesFixtureMetadataAndRespectsScopeAndState(t *testing.T) {
	const data = "QFI\xfb\x00\x01payload"
	const checksum = "281a54781cf7c2b024e7755ac5be21d3"
	seed := domain.Image{ID: "image", ProjectID: "owner"}
	service := app.New(seed, time.Now, data, nil)
	metadata, err := service.Get(seed.ProjectID, seed.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(len(data)), metadata.Size)
	assert.Equal(t, checksum, metadata.Checksum)

	downloaded, digest, err := service.Download(seed.ProjectID, seed.ID)
	require.NoError(t, err)
	assert.Equal(t, data, downloaded)
	assert.Equal(t, metadata.Checksum, digest)
	downloaded, digest, err = service.Download("foreign", seed.ID)
	assert.ErrorIs(t, err, app.ErrImageNotFound)
	assert.Empty(t, downloaded)
	assert.Empty(t, digest)
	_, _, err = service.Download(seed.ProjectID, "unknown")
	assert.ErrorIs(t, err, app.ErrImageNotFound)

	require.NoError(t, service.SetDeactivated(seed.ID, true))
	deactivated, err := service.Get(seed.ProjectID, seed.ID)
	require.NoError(t, err)
	assert.Equal(t, metadata.Size, deactivated.Size)
	assert.Equal(t, metadata.Checksum, deactivated.Checksum)
	downloaded, digest, err = service.Download(seed.ProjectID, seed.ID)
	assert.ErrorIs(t, err, app.ErrImageDeactivated)
	assert.Empty(t, downloaded)
	assert.Empty(t, digest)
	_, _, err = service.Download("foreign", seed.ID)
	assert.ErrorIs(t, err, app.ErrImageNotFound)

	service.Reset()
	downloaded, digest, err = service.Download(seed.ProjectID, seed.ID)
	require.NoError(t, err)
	assert.Equal(t, data, downloaded)
	assert.Equal(t, metadata.Checksum, digest)
}

func TestDownloadWithNoDataReturnsEmptyBodyAndChecksum(t *testing.T) {
	seed := domain.Image{ID: "image", ProjectID: "owner", Size: 4, Checksum: "old checksum"}
	service := app.New(seed, time.Now, "", nil)
	metadata, err := service.Get(seed.ProjectID, seed.ID)
	require.NoError(t, err)
	assert.Zero(t, metadata.Size)
	assert.Empty(t, metadata.Checksum)

	data, checksum, err := service.Download(seed.ProjectID, seed.ID)
	require.NoError(t, err)
	assert.Empty(t, data)
	assert.Empty(t, checksum)
}

func TestConcurrentDownloadsConsumeOnlyOneFailure(t *testing.T) {
	const data = "fixture image bytes"
	seed := domain.Image{ID: "image", ProjectID: "owner"}
	service := app.New(seed, time.Now, data, nil)
	assert.ErrorIs(t, service.SetUnavailableDownload("missing", true), app.ErrImageNotFound)
	require.NoError(t, service.SetUnavailableDownload(seed.ID, true))
	require.NoError(t, service.SetUnavailableDownload(seed.ID, true))
	size, err := service.CheckDownload(seed.ProjectID, seed.ID)
	require.NoError(t, err)
	assert.Equal(t, len(data), size)
	_, _, err = service.Download("foreign", seed.ID)
	assert.ErrorIs(t, err, app.ErrImageNotFound)
	require.NoError(t, service.SetDeactivated(seed.ID, true))
	_, err = service.CheckDownload(seed.ProjectID, seed.ID)
	assert.ErrorIs(t, err, app.ErrImageDeactivated)
	_, _, err = service.Download(seed.ProjectID, seed.ID)
	assert.ErrorIs(t, err, app.ErrImageDeactivated)
	require.NoError(t, service.SetDeactivated(seed.ID, false))

	const attempts = 16
	type result struct {
		data string
		err  error
	}
	start := make(chan struct{})
	results := make(chan result, attempts)
	for range attempts {
		go func() {
			<-start
			downloaded, _, err := service.Download(seed.ProjectID, seed.ID)
			results <- result{data: downloaded, err: err}
		}()
	}
	close(start)
	failures := 0
	for range attempts {
		response := <-results
		if errors.Is(response.err, app.ErrDownloadUnavailable) {
			failures++
			assert.Empty(t, response.data)
			continue
		}
		assert.NoError(t, response.err)
		assert.Equal(t, data, response.data)
	}
	assert.Equal(t, 1, failures)

	require.NoError(t, service.SetUnavailableDownload(seed.ID, true))
	require.NoError(t, service.SetUnavailableDownload(seed.ID, false))
	_, _, err = service.Download(seed.ProjectID, seed.ID)
	assert.NoError(t, err)
	require.NoError(t, service.SetUnavailableDownload(seed.ID, true))
	service.Reset()
	_, _, err = service.Download(seed.ProjectID, seed.ID)
	assert.NoError(t, err)
}
