package volume_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	app "github.com/JSYoo5B/sandstack/internal/app/volume"
	domain "github.com/JSYoo5B/sandstack/internal/domain/volume"
)

func TestCloneVolumeCapturesPublishedImageSourceAndLeavesSourceIndependent(t *testing.T) {
	seed := domain.Volume{
		ID: fixtureID, ProjectID: "project", Size: 5, State: domain.StateInUse, Bootable: true,
		ImageSource: domain.ImageSource{ID: "image", Name: "source image"}, ImageMetadataReady: true,
		Attachment: domain.Attachment{ID: "attachment", VolumeID: fixtureID, ServerID: "server"},
		Metadata:   map[string]string{"source": "preserved"},
	}
	service := app.New(seed, time.Now, func() string { return createdID })
	require.NoError(t, service.SetUnavailableDetail(seed.ID, true))

	created, err := service.CreateFromVolume(seed.ProjectID, seed.ID, "clone", "", 0, map[string]string{"new": "metadata"})
	require.NoError(t, err)
	assert.Equal(t, seed.ID, created.SourceVolumeID)
	assert.Equal(t, seed.Size, created.Size)
	assert.Equal(t, domain.StateCreating, created.State)
	assert.True(t, created.Bootable)
	assert.False(t, created.ImageMetadataReady)
	assert.Equal(t, seed.ImageSource, created.ImageSource)
	assert.Empty(t, created.Attachment)
	assert.Equal(t, map[string]string{"new": "metadata"}, created.Metadata)
	_, err = service.Get(seed.ProjectID, seed.ID)
	assert.ErrorIs(t, err, app.ErrDetailUnavailable)
	current, err := service.Get(seed.ProjectID, seed.ID)
	require.NoError(t, err)
	assert.Equal(t, seed, current)

	require.NoError(t, service.Detach(seed.ProjectID, seed.Attachment.ServerID, seed.ID))
	require.NoError(t, service.Delete(seed.ProjectID, seed.ID))
	require.NoError(t, service.CompleteCreation(created.ID))
	available, err := service.Get(seed.ProjectID, created.ID)
	require.NoError(t, err)
	assert.True(t, available.Bootable)
	assert.True(t, available.ImageMetadataReady)
	assert.Equal(t, seed.ImageSource, available.ImageSource)
}

func TestCloneVolumeValidatesSourceAndDoesNotPublishUnreadyImageMetadata(t *testing.T) {
	seed := domain.Volume{
		ID: fixtureID, ProjectID: "project", Size: 5, State: domain.StateAvailable, Bootable: true,
		ImageSource: domain.ImageSource{ID: "not published"},
	}
	service := app.New(seed, time.Now, func() string { return createdID })
	_, err := service.CreateFromVolume("foreign", seed.ID, "clone", "", 5, nil)
	assert.ErrorIs(t, err, app.ErrVolumeSourceNotFound)
	_, err = service.CreateFromVolume(seed.ProjectID, "missing", "clone", "", 5, nil)
	assert.ErrorIs(t, err, app.ErrVolumeSourceNotFound)
	_, err = service.CreateFromVolume(seed.ProjectID, seed.ID, "clone", "", 4, nil)
	assert.ErrorIs(t, err, app.ErrVolumeSmallerThanSource)
	_, err = service.CreateFromVolume(seed.ProjectID, seed.ID, "clone", "", -1, nil)
	assert.ErrorIs(t, err, app.ErrInvalidVolumeSize)

	created, err := service.CreateFromVolume(seed.ProjectID, seed.ID, "clone", "", 6, nil)
	require.NoError(t, err)
	assert.Equal(t, 6, created.Size)
	assert.True(t, created.Bootable)
	assert.Empty(t, created.ImageSource)
	_, err = service.CreateFromVolume(seed.ProjectID, created.ID, "too early", "", 6, nil)
	assert.ErrorIs(t, err, app.ErrVolumeSourceState)
	require.NoError(t, service.CompleteCreation(created.ID))
	current, err := service.Get(seed.ProjectID, created.ID)
	require.NoError(t, err)
	assert.True(t, current.Bootable)
	assert.False(t, current.ImageMetadataReady)
	require.NoError(t, service.SetFailed(created.ID, true))
	_, err = service.CreateFromVolume(seed.ProjectID, created.ID, "error source", "", 6, nil)
	assert.ErrorIs(t, err, app.ErrVolumeSourceState)
}
