package image_test

import (
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
	service := app.New(seed, time.Now, data)
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
	service := app.New(seed, time.Now, "")
	metadata, err := service.Get(seed.ProjectID, seed.ID)
	require.NoError(t, err)
	assert.Zero(t, metadata.Size)
	assert.Empty(t, metadata.Checksum)

	data, checksum, err := service.Download(seed.ProjectID, seed.ID)
	require.NoError(t, err)
	assert.Empty(t, data)
	assert.Empty(t, checksum)
}
