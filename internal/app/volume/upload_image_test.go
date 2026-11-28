package volume_test

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	appimage "github.com/JSYoo5B/sandstack/internal/app/image"
	app "github.com/JSYoo5B/sandstack/internal/app/volume"
	imagedomain "github.com/JSYoo5B/sandstack/internal/domain/image"
	domain "github.com/JSYoo5B/sandstack/internal/domain/volume"
)

const (
	uploadSourceImageID = "00000a00-0000-4000-8000-000000000000"
	uploadSourceData    = "source image bytes"
)

type uploadFixture struct {
	volumes *app.Service
	images  *appimage.Service
	seed    domain.Volume
	start   time.Time
	now     *time.Time
}

func newUploadFixture(t *testing.T) uploadFixture {
	t.Helper()
	start := time.Date(2026, time.November, 28, 3, 4, 5, 0, time.UTC)
	now := start
	clock := func() time.Time { return now }
	id := 0
	newID := func() string {
		id++
		return fmt.Sprintf("%08x-0000-4000-8000-000000000000", id)
	}
	images := appimage.New(imagedomain.Image{
		ID: uploadSourceImageID, ProjectID: "project", Name: "source",
		ContainerFormat: imagedomain.ContainerFormatBare, DiskFormat: imagedomain.DiskFormatQCOW2,
	}, clock, uploadSourceData, newID)
	seed := domain.Volume{
		ID: fixtureID, ProjectID: "project", Size: 1, State: domain.StateAvailable, UpdatedAt: start,
		Description: domain.NullableText{Value: "seed description", Valid: true},
	}
	volumes := app.NewWithImageLookup(seed, clock, newID, images).WithImageUploads(images)
	return uploadFixture{volumes: volumes, images: images, seed: seed, start: start, now: &now}
}

func md5Hex(data string) string {
	digest := md5.Sum([]byte(data))
	return hex.EncodeToString(digest[:])
}

// https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/volume/api.py#L1483-L1552
// https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/volume/manager.py#L1752-L1803
func TestUploadImageCreatesActiveImageAndRestoresAvailableVolume(t *testing.T) {
	fixture := newUploadFixture(t)
	*fixture.now = fixture.start.Add(time.Minute)

	uploaded, err := fixture.volumes.UploadImage(fixture.seed.ProjectID, fixture.seed.ID, app.UploadImageRequest{
		ImageName: "blank upload", ContainerFormat: "bare", DiskFormat: "raw",
	})

	require.NoError(t, err)
	assert.Equal(t, fixture.seed, uploaded.Volume)
	assert.Equal(t, "standard", uploaded.VolumeType.Name)
	assert.Equal(t, "queued", uploaded.Image.Status())
	assert.Equal(t, "blank upload", uploaded.Image.Name)
	image, err := fixture.images.Get(fixture.seed.ProjectID, uploaded.Image.ID)
	require.NoError(t, err)
	assert.Equal(t, "active", image.Status())
	assert.Equal(t, "bare", image.ContainerFormat)
	assert.Equal(t, "raw", image.DiskFormat)
	assert.Equal(t, int64(0), image.Size)
	assert.Equal(t, md5Hex(""), image.Checksum)
	assert.Empty(t, image.Properties)
	assert.False(t, image.Protected)
	volume, err := fixture.volumes.Get(fixture.seed.ProjectID, fixture.seed.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.StateAvailable, volume.State)
	assert.Equal(t, *fixture.now, volume.UpdatedAt)
}

// https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/image/image_utils.py#L1161-L1239
func TestUploadImageCopiesSourceImageDataOfImageVolume(t *testing.T) {
	fixture := newUploadFixture(t)
	created, err := fixture.volumes.CreateFromImage(fixture.seed.ProjectID, uploadSourceImageID, "from image", "", 1, nil)
	require.NoError(t, err)
	require.NoError(t, fixture.volumes.CompleteCreation(created.ID))

	uploaded, err := fixture.volumes.UploadImage(fixture.seed.ProjectID, created.ID, app.UploadImageRequest{
		ImageName: "image copy", ContainerFormat: "ovf", DiskFormat: "vmdk",
	})

	require.NoError(t, err)
	image, err := fixture.images.Get(fixture.seed.ProjectID, uploaded.Image.ID)
	require.NoError(t, err)
	assert.Equal(t, "active", image.Status())
	assert.Equal(t, "ovf", image.ContainerFormat)
	assert.Equal(t, "vmdk", image.DiskFormat)
	assert.Equal(t, int64(len(uploadSourceData)), image.Size)
	assert.Equal(t, md5Hex(uploadSourceData), image.Checksum)
	assert.Empty(t, image.Properties)
	data, checksum, err := fixture.images.Download(fixture.seed.ProjectID, image.ID)
	require.NoError(t, err)
	assert.Equal(t, uploadSourceData, data)
	assert.Equal(t, image.Checksum, checksum)
}

// https://github.com/openstack/glance/blob/12.0.0/glance/domain/__init__.py#L151-L163
// https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/volume/manager.py#L1784-L1802
func TestUploadImageWithoutContainerFormatDeletesFailedImage(t *testing.T) {
	fixture := newUploadFixture(t)

	uploaded, err := fixture.volumes.UploadImage(fixture.seed.ProjectID, fixture.seed.ID, app.UploadImageRequest{
		ImageName: "no container", DiskFormat: "raw",
	})

	require.NoError(t, err)
	assert.Empty(t, uploaded.Image.ContainerFormat)
	_, err = fixture.images.Get(fixture.seed.ProjectID, uploaded.Image.ID)
	assert.ErrorIs(t, err, appimage.ErrImageNotFound)
	volume, err := fixture.volumes.Get(fixture.seed.ProjectID, fixture.seed.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.StateAvailable, volume.State)
}

// https://github.com/openstack/cinder/blob/134790e4206f4effa503c47965b7d0cc888695a1/cinder/volume/api.py#L1488-L1506
// https://github.com/openstack/glance/blob/12.0.0/glance/common/config.py#L44-L56
func TestUploadImageRejectsStateAndFormatsWithoutChanges(t *testing.T) {
	fixture := newUploadFixture(t)
	creating, err := fixture.volumes.Create(fixture.seed.ProjectID, "", "", 1)
	require.NoError(t, err)
	inUse := domain.Volume{
		ID: fixtureID, ProjectID: "project", Size: 1, State: domain.StateInUse,
		Attachment: domain.Attachment{ID: "attachment", VolumeID: fixtureID, ServerID: "server"},
	}
	attached := app.New(inUse, time.Now, nil).WithImageUploads(fixture.images)
	valid := app.UploadImageRequest{ImageName: "image", ContainerFormat: "bare", DiskFormat: "raw"}
	forced := valid
	forced.Force = true
	unknownContainer := valid
	unknownContainer.ContainerFormat = "compressed"
	unknownDisk := valid
	unknownDisk.DiskFormat = "ploop"
	before := fixture.images.List(fixture.seed.ProjectID, appimage.ListFilter{})

	_, foreignErr := fixture.volumes.UploadImage("foreign", fixture.seed.ID, valid)
	_, missingErr := fixture.volumes.UploadImage(fixture.seed.ProjectID, "missing", valid)
	_, creatingErr := fixture.volumes.UploadImage(fixture.seed.ProjectID, creating.ID, valid)
	_, inUseErr := attached.UploadImage(inUse.ProjectID, inUse.ID, valid)
	_, forcedErr := attached.UploadImage(inUse.ProjectID, inUse.ID, forced)
	_, containerErr := fixture.volumes.UploadImage(fixture.seed.ProjectID, fixture.seed.ID, unknownContainer)
	_, diskErr := fixture.volumes.UploadImage(fixture.seed.ProjectID, fixture.seed.ID, unknownDisk)
	_, unavailableErr := app.New(fixture.seed, time.Now, nil).UploadImage(fixture.seed.ProjectID, fixture.seed.ID, valid)

	assert.ErrorIs(t, foreignErr, app.ErrVolumeNotFound)
	assert.ErrorIs(t, missingErr, app.ErrVolumeNotFound)
	assert.ErrorIs(t, creatingErr, app.ErrVolumeStateConflict)
	assert.ErrorIs(t, inUseErr, app.ErrVolumeStateConflict)
	assert.ErrorIs(t, forcedErr, app.ErrVolumeStateConflict)
	assert.ErrorIs(t, containerErr, app.ErrUploadImageRejected)
	assert.ErrorIs(t, diskErr, appimage.ErrImageFormat)
	assert.ErrorIs(t, unavailableErr, app.ErrUploadImageUnavailable)
	assert.Equal(t, before, fixture.images.List(fixture.seed.ProjectID, appimage.ListFilter{}))
	volume, err := fixture.volumes.Get(fixture.seed.ProjectID, fixture.seed.ID)
	require.NoError(t, err)
	assert.Equal(t, fixture.seed, volume)
	stillAttached, err := attached.Get(inUse.ProjectID, inUse.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.StateInUse, stillAttached.State)
}
