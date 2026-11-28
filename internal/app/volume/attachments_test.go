package volume_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	app "github.com/JSYoo5B/sandstack/internal/app/volume"
	domain "github.com/JSYoo5B/sandstack/internal/domain/volume"
)

func TestAttachmentTransitionsProtectVolumeUntilDetach(t *testing.T) {
	start := time.Date(2026, time.November, 25, 0, 0, 0, 0, time.UTC)
	now := start.In(time.FixedZone("fixture", 3600))
	seed := domain.Volume{ID: fixtureID, ProjectID: "project", State: domain.StateAvailable}
	ids := []string{seed.ID, createdID}
	service := app.New(seed, func() time.Time { return now }, func() string {
		id := ids[0]
		ids = ids[1:]
		return id
	})
	_, err := service.GetAttachment(seed.ProjectID, "server", seed.ID)
	assert.ErrorIs(t, err, app.ErrAttachmentNotFound)
	_, err = service.Attach("other-project", "server", seed.ID, "")
	assert.ErrorIs(t, err, app.ErrVolumeNotFound)
	_, err = service.Attach(seed.ProjectID, "server", "other-volume", "")
	assert.ErrorIs(t, err, app.ErrVolumeNotFound)

	attachment, err := service.Attach(seed.ProjectID, "server", seed.ID, "")
	require.NoError(t, err)
	assert.Equal(t, domain.Attachment{
		ID: createdID, VolumeID: seed.ID, ServerID: "server", Device: "/dev/vdb", AttachedAt: start,
	}, attachment)
	volume, err := service.Get(seed.ProjectID, seed.ID)
	require.NoError(t, err)
	assert.Equal(t, attachment, volume.Attachment)
	assert.Equal(t, domain.StateInUse, volume.State)
	assert.Equal(t, start, volume.UpdatedAt)
	assert.Equal(t, []domain.Attachment{attachment}, service.ListAttachments(seed.ProjectID, "server"))
	assert.Empty(t, service.ListAttachments("other-project", "server"))
	assert.Empty(t, service.ListAttachments(seed.ProjectID, "another-server"))

	_, err = service.GetAttachment(seed.ProjectID, "another-server", seed.ID)
	assert.ErrorIs(t, err, app.ErrAttachmentNotFound)
	assert.ErrorIs(t, service.Detach(seed.ProjectID, "another-server", seed.ID), app.ErrAttachmentNotFound)
	_, err = service.Attach(seed.ProjectID, "server", seed.ID, "")
	assert.ErrorIs(t, err, app.ErrVolumeStateConflict)
	assert.ErrorIs(t, service.Delete(seed.ProjectID, seed.ID), app.ErrVolumeStateConflict)
	assert.ErrorIs(t, service.SetFailed(seed.ID, true), app.ErrVolumeStateConflict)
	assert.ErrorIs(t, service.CompleteCreation(seed.ID), app.ErrVolumeStateConflict)
	require.NoError(t, service.SetFailed(seed.ID, false))
	loaded, err := service.GetAttachment(seed.ProjectID, "server", seed.ID)
	require.NoError(t, err)
	assert.Equal(t, attachment, loaded)

	now = now.Add(time.Minute)
	require.NoError(t, service.Detach(seed.ProjectID, "server", seed.ID))
	assert.ErrorIs(t, service.Detach(seed.ProjectID, "server", seed.ID), app.ErrAttachmentNotFound)
	volume, err = service.Get(seed.ProjectID, seed.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.Attachment{}, volume.Attachment)
	assert.Equal(t, domain.StateAvailable, volume.State)
	assert.Equal(t, start.Add(time.Minute), volume.UpdatedAt)
	assert.Empty(t, service.ListAttachments(seed.ProjectID, "server"))
	assert.NoError(t, service.Delete(seed.ProjectID, seed.ID))
}

func TestAttachmentDeviceSelectionAndReset(t *testing.T) {
	seed := domain.Volume{ID: fixtureID, ProjectID: "project", State: domain.StateAvailable}
	id := 32
	service := app.New(seed, time.Now, func() string {
		id++
		return fmt.Sprintf("%08x-0000-4000-8000-000000000000", id)
	})
	const serverID = "server"
	_, err := service.Attach(seed.ProjectID, serverID, seed.ID, "/dev/vda")
	assert.ErrorIs(t, err, app.ErrInvalidAttachmentDevice)
	first, err := service.Attach(seed.ProjectID, serverID, seed.ID, "/dev/vdc")
	require.NoError(t, err)
	assert.Equal(t, "/dev/vdc", first.Device)

	second, err := service.Create(seed.ProjectID, seed.UserID, "second", "", 1)
	require.NoError(t, err)
	_, err = service.Attach(seed.ProjectID, serverID, second.ID, "")
	assert.ErrorIs(t, err, app.ErrVolumeStateConflict)
	require.NoError(t, service.CompleteCreation(second.ID))
	_, err = service.Attach(seed.ProjectID, serverID, second.ID, "/dev/vdc")
	assert.ErrorIs(t, err, app.ErrAttachmentDeviceConflict)
	secondAttachment, err := service.Attach(seed.ProjectID, serverID, second.ID, "")
	require.NoError(t, err)
	assert.Equal(t, "/dev/vdb", secondAttachment.Device)
	assert.Equal(t, []domain.Attachment{first, secondAttachment}, service.ListAttachments(seed.ProjectID, serverID))

	require.NoError(t, service.Detach(seed.ProjectID, serverID, second.ID))
	secondAttachment, err = service.Attach(seed.ProjectID, "another-server", second.ID, "/dev/vdc")
	require.NoError(t, err)
	assert.Equal(t, "/dev/vdc", secondAttachment.Device)
	assert.Equal(t, []domain.Attachment{first}, service.ListAttachments(seed.ProjectID, serverID))

	service.Reset()
	assert.Empty(t, service.ListAttachments(seed.ProjectID, serverID))
	assert.Empty(t, service.ListAttachments(seed.ProjectID, "another-server"))
	assert.Equal(t, []domain.Volume{seed}, service.List(seed.ProjectID, app.ListFilter{}))
}

func TestAttachmentsRejectExhaustedDeviceSlots(t *testing.T) {
	seed := domain.Volume{ID: fixtureID, ProjectID: "project"}
	id := 32
	service := app.New(seed, time.Now, func() string {
		id++
		return fmt.Sprintf("%08x-0000-4000-8000-000000000000", id)
	})
	for letter := byte('b'); letter <= 'z'; letter++ {
		volume, err := service.Create(seed.ProjectID, seed.UserID, "", "", 1)
		require.NoError(t, err)
		require.NoError(t, service.CompleteCreation(volume.ID))
		attachment, err := service.Attach(seed.ProjectID, "server", volume.ID, "")
		require.NoError(t, err)
		assert.Equal(t, "/dev/vd"+string(letter), attachment.Device)
	}

	_, err := service.Attach(seed.ProjectID, "server", seed.ID, "")
	assert.ErrorIs(t, err, app.ErrAttachmentDeviceConflict)
	assert.Len(t, service.ListAttachments(seed.ProjectID, "server"), 25)
}
