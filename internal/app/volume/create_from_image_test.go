package volume_test

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	app "github.com/JSYoo5B/sandstack/internal/app/volume"
	imagedomain "github.com/JSYoo5B/sandstack/internal/domain/image"
	domain "github.com/JSYoo5B/sandstack/internal/domain/volume"
)

type imageLookupFunc func(projectID, imageID string) (imagedomain.Image, error)

func (lookup imageLookupFunc) Get(projectID, imageID string) (imagedomain.Image, error) {
	return lookup(projectID, imageID)
}

func TestImageVolumeProvisioningCapturesSourceAndBecomesBootableOnSuccess(t *testing.T) {
	seed := domain.Volume{ID: fixtureID, ProjectID: "project", UserID: "user"}
	image := imagedomain.Image{
		ID: "image", ProjectID: seed.ProjectID, Name: "original", Checksum: "source-checksum",
		Size: 1<<30 + 1, MinDiskGiB: 2, MinRAMMiB: 256,
		ContainerFormat: imagedomain.ContainerFormatBare, DiskFormat: imagedomain.DiskFormatQCOW2,
	}
	ids := []string{createdID, "00000003-0000-4000-8000-000000000000"}
	now := time.Date(2026, time.November, 25, 12, 0, 0, 0, time.UTC)
	service := app.NewWithImageLookup(seed, func() time.Time { return now }, func() string {
		id := ids[0]
		ids = ids[1:]
		return id
	}, imageLookupFunc(func(projectID, imageID string) (imagedomain.Image, error) {
		assert.Equal(t, seed.ProjectID, projectID)
		assert.Equal(t, image.ID, imageID)
		return image, nil
	}))

	metadata := map[string]string{"stage": "initial"}
	provisioning, err := service.CreateFromImage(seed.ProjectID, image.ID, "boot", "description", 2, metadata)
	require.NoError(t, err)
	assert.Equal(t, domain.StateCreating, provisioning.State)
	assert.False(t, provisioning.Bootable)
	assert.False(t, provisioning.ImageMetadataReady)
	assert.Equal(t, now, provisioning.CreatedAt)
	assert.Equal(t, domain.ImageSource{
		ID: image.ID, Name: image.Name, Checksum: image.Checksum, Size: image.Size,
		ContainerFormat: image.ContainerFormat, DiskFormat: image.DiskFormat,
		MinDiskGiB: image.MinDiskGiB, MinRAMMiB: image.MinRAMMiB,
	}, provisioning.ImageSource)
	image.Name = "renamed after lookup"
	metadata["stage"] = "changed caller map"
	now = now.Add(time.Minute)
	require.NoError(t, service.CompleteCreation(provisioning.ID))
	available, err := service.Get(seed.ProjectID, provisioning.ID)
	require.NoError(t, err)
	assert.True(t, available.Bootable)
	assert.True(t, available.ImageMetadataReady)
	assert.Equal(t, domain.StateAvailable, available.State)
	assert.Equal(t, now, available.UpdatedAt)
	assert.Equal(t, provisioning.ImageSource, available.ImageSource)
	assert.Equal(t, map[string]string{"stage": "initial"}, available.Metadata)

	second, err := service.CreateFromImage(seed.ProjectID, image.ID, "fail", "", 2, nil)
	require.NoError(t, err)
	require.NoError(t, service.SetFailed(second.ID, true))
	assert.ErrorIs(t, service.CompleteCreation(second.ID), app.ErrVolumeStateConflict)
	failed, err := service.Get(seed.ProjectID, second.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.StateError, failed.State)
	assert.False(t, failed.Bootable)
	assert.False(t, failed.ImageMetadataReady)
}

func TestImageVolumeProvisioningChecksVisibilityStateAndSize(t *testing.T) {
	seed := domain.Volume{ID: fixtureID, ProjectID: "project"}
	const imageID = "image"
	for _, test := range []struct {
		name    string
		image   imagedomain.Image
		getErr  error
		size    int
		wantErr error
	}{
		{name: "unknown image", getErr: errors.New("missing"), size: 1, wantErr: app.ErrVolumeSourceImageUnavailable},
		{name: "foreign image", image: imagedomain.Image{ProjectID: "foreign"}, size: 1, wantErr: app.ErrVolumeSourceImageUnavailable},
		{name: "queued image", image: imagedomain.Image{Queued: true}, size: 1, wantErr: app.ErrVolumeSourceImageUnavailable},
		{name: "deactivated image", image: imagedomain.Image{Deactivated: true}, size: 1, wantErr: app.ErrVolumeSourceImageUnavailable},
		{name: "image byte ceiling", image: imagedomain.Image{Size: 1<<30 + 1}, size: 1, wantErr: app.ErrVolumeSourceImageSize},
		{name: "minimum disk", image: imagedomain.Image{MinDiskGiB: 2}, size: 1, wantErr: app.ErrVolumeSourceImageSize},
		{name: "nonpositive size", wantErr: app.ErrInvalidVolumeSize},
	} {
		t.Run(test.name, func(t *testing.T) {
			image := test.image
			image.ID = imageID
			if image.ProjectID == "" {
				image.ProjectID = seed.ProjectID
			}
			service := app.NewWithImageLookup(seed, time.Now, nil, imageLookupFunc(func(_, _ string) (imagedomain.Image, error) {
				return image, test.getErr
			}))

			_, err := service.CreateFromImage(seed.ProjectID, imageID, "boot", "", test.size, nil)
			assert.ErrorIs(t, err, test.wantErr)
			assert.Len(t, service.List(seed.ProjectID, app.ListFilter{}), 1)
		})
	}
}
