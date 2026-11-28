package volume_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	app "github.com/JSYoo5B/sandstack/internal/app/volume"
	imagedomain "github.com/JSYoo5B/sandstack/internal/domain/image"
	domain "github.com/JSYoo5B/sandstack/internal/domain/volume"
)

func TestSetBootableRefreshesTimestampAndKeepsDetailFailure(t *testing.T) {
	start := time.Date(2026, time.November, 25, 12, 0, 0, 0, time.UTC)
	now := start.Add(time.Minute).In(time.FixedZone("fixture", 3600))
	seed := domain.Volume{
		ID: fixtureID, ProjectID: "project", State: domain.StateInUse, UpdatedAt: start,
		Attachment: domain.Attachment{ID: "attachment", VolumeID: fixtureID, ServerID: "server"},
		Bootable:   true, ImageMetadataReady: true, ImageSource: domain.ImageSource{ID: "image"},
		Metadata: map[string]string{"tag": "kept"},
	}
	service := app.New(seed, func() time.Time { return now }, nil)
	require.NoError(t, service.SetUnavailableDetail(seed.ID, true))
	assert.ErrorIs(t, service.SetBootable("foreign", seed.ID, false), app.ErrVolumeNotFound)
	assert.ErrorIs(t, service.SetBootable(seed.ProjectID, "missing", true), app.ErrVolumeNotFound)
	assert.Equal(t, []domain.Volume{seed}, service.List(seed.ProjectID, app.ListFilter{}))

	require.NoError(t, service.SetBootable(seed.ProjectID, seed.ID, true))
	want := seed
	want.UpdatedAt = now.UTC()
	assert.Equal(t, []domain.Volume{want}, service.List(seed.ProjectID, app.ListFilter{}))
	now = now.Add(time.Minute)
	require.NoError(t, service.SetBootable(seed.ProjectID, seed.ID, false))
	_, err := service.Get(seed.ProjectID, seed.ID)
	assert.ErrorIs(t, err, app.ErrDetailUnavailable)
	updated, err := service.Get(seed.ProjectID, seed.ID)
	require.NoError(t, err)
	want.Bootable = false
	want.UpdatedAt = now.UTC()
	assert.Equal(t, want, updated)
}

func TestVolumeProvisioningReassertsCapturedBootability(t *testing.T) {
	seed := domain.Volume{ID: fixtureID, ProjectID: "project", Size: 1, Bootable: true}
	image := imagedomain.Image{ID: "image", ProjectID: seed.ProjectID}
	ids := []string{createdID, "00000003-0000-4000-8000-000000000000", "00000004-0000-4000-8000-000000000000"}
	service := app.NewWithImageLookup(seed, time.Now, func() string {
		id := ids[0]
		ids = ids[1:]
		return id
	}, imageLookupFunc(func(_, _ string) (imagedomain.Image, error) { return image, nil }))
	fromImage, err := service.CreateFromImage(seed.ProjectID, seed.UserID, image.ID, "image", "", 1, nil)
	require.NoError(t, err)
	clone, err := service.CreateFromVolume(seed.ProjectID, seed.UserID, seed.ID, "clone", "", 0, nil)
	require.NoError(t, err)
	blank, err := service.Create(seed.ProjectID, seed.UserID, "blank", "", 1)
	require.NoError(t, err)
	assert.True(t, fromImage.BootableOnProvision)
	assert.True(t, clone.BootableOnProvision)
	assert.False(t, blank.BootableOnProvision)
	assert.False(t, fromImage.Bootable)
	assert.True(t, clone.Bootable)

	require.NoError(t, service.SetBootable(seed.ProjectID, fromImage.ID, false))
	require.NoError(t, service.SetBootable(seed.ProjectID, clone.ID, false))
	require.NoError(t, service.SetBootable(seed.ProjectID, blank.ID, true))
	for _, id := range []string{fromImage.ID, clone.ID, blank.ID} {
		require.NoError(t, service.CompleteCreation(id))
		available, err := service.Get(seed.ProjectID, id)
		require.NoError(t, err)
		assert.True(t, available.Bootable)
		assert.False(t, available.BootableOnProvision)
		assert.Equal(t, id == fromImage.ID, available.ImageMetadataReady)
	}
}
