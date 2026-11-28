package compute_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	app "github.com/JSYoo5B/sandstack/internal/app/compute"
	domain "github.com/JSYoo5B/sandstack/internal/domain/compute"
)

func TestLockAndUnlockRecordLockState(t *testing.T) {
	start := time.Date(2026, time.November, 25, 1, 2, 3, 0, time.UTC)
	now := start
	fault := &domain.Fault{Code: 500, CreatedAt: start}
	seed := domain.Server{
		ID: "server", ProjectID: "project", UserID: "user", Name: "fixture",
		CreatedAt: start, UpdatedAt: start, State: domain.StatusShutoff, Fault: fault,
	}
	service := app.New(seed, func() time.Time { return now }, nil)
	read := func() domain.Server {
		t.Helper()
		current, err := service.Get(seed.ProjectID, seed.ID)
		require.NoError(t, err)
		return current.Server
	}

	assert.ErrorIs(t, service.Lock("foreign", seed.ID), app.ErrServerNotFound)
	assert.ErrorIs(t, service.Unlock(seed.ProjectID, "other"), app.ErrServerNotFound)
	now = start.Add(time.Minute)
	lockErr := service.Lock(seed.ProjectID, seed.ID)
	now = start.Add(2 * time.Minute)
	repeatErr := service.Lock(seed.ProjectID, seed.ID)

	require.NoError(t, lockErr)
	require.NoError(t, repeatErr)
	locked := seed
	locked.Locked = true
	locked.UpdatedAt = start.Add(time.Minute)
	assert.Equal(t, locked, read())

	now = start.Add(3 * time.Minute)
	unlockErr := service.Unlock(seed.ProjectID, seed.ID)
	now = start.Add(4 * time.Minute)
	repeatUnlockErr := service.Unlock(seed.ProjectID, seed.ID)

	require.NoError(t, unlockErr)
	require.NoError(t, repeatUnlockErr)
	unlocked := seed
	unlocked.UpdatedAt = start.Add(4 * time.Minute)
	assert.Equal(t, unlocked, read())

	require.NoError(t, service.Lock(seed.ProjectID, seed.ID))
	service.Reset()

	assert.Equal(t, seed, read())
}

func TestLockedServerRejectsGuardedChanges(t *testing.T) {
	start := time.Date(2026, time.November, 25, 1, 2, 3, 0, time.UTC)
	seed := domain.Server{
		ID: "server", ProjectID: "project", Name: "fixture", UpdatedAt: start,
		Metadata: map[string]string{"item": "value"},
	}
	volumes := &recordedAttachments{seededAttachments: seededAttachments{{VolumeID: "volume"}}}
	service := app.New(seed, func() time.Time { return start.Add(time.Minute) }, volumes)
	attachments := app.NewAttachments(service, volumes)
	require.NoError(t, service.Lock(seed.ProjectID, seed.ID))
	id, project := seed.ID, seed.ProjectID
	name := "renamed"

	guarded := map[string]error{
		"stop":            service.SetPower(project, id, false),
		"start":           service.SetPower(project, id, true),
		"soft reboot":     service.Reboot(project, id, false),
		"hard reboot":     service.Reboot(project, id, true),
		"pause":           service.Pause(project, id),
		"unpause":         service.Unpause(project, id),
		"suspend":         service.Suspend(project, id),
		"resume":          service.Resume(project, id),
		"put metadatum":   service.PutMetadatum(project, id, "item", "changed"),
		"delete item":     service.DeleteMetadatum(project, id, "item"),
		"detach attached": attachments.Detach(project, id, "volume"),
	}
	_, replaceErr := service.ReplaceMetadata(project, id, map[string]string{})
	_, mergeErr := service.MergeMetadata(project, id, map[string]string{"other": "value"})
	_, attachErr := attachments.Attach(project, id, "other", "")
	missingItemErr := service.DeleteMetadatum(project, id, "missing")
	missingAttachmentErr := attachments.Detach(project, id, "missing")
	_, renameErr := service.UpdateName(project, id, &name)

	for action, err := range guarded {
		assert.ErrorIs(t, err, app.ErrServerLocked, action)
	}
	assert.ErrorIs(t, replaceErr, app.ErrServerLocked)
	assert.ErrorIs(t, mergeErr, app.ErrServerLocked)
	assert.ErrorIs(t, attachErr, app.ErrServerLocked)
	assert.ErrorIs(t, missingItemErr, app.ErrServerMetadatumNotFound)
	assert.ErrorIs(t, missingAttachmentErr, errAttachmentMissing)
	assert.NoError(t, renameErr)
	assert.Empty(t, volumes.attached)
	assert.Empty(t, volumes.detached)
	current, err := service.Get(project, id)
	require.NoError(t, err)
	assert.Equal(t, domain.StatusActive, current.Status())
	assert.Equal(t, map[string]string{"item": "value"}, current.Metadata)
	assert.Equal(t, name, current.Name)

	require.NoError(t, service.Unlock(project, id))
	require.NoError(t, service.Pause(project, id))
	require.NoError(t, service.PutMetadatum(project, id, "item", "changed"))
	require.NoError(t, attachments.Detach(project, id, "volume"))

	paused := domain.StatusPaused
	assert.Len(t, service.List(project, app.ListFilter{Status: &paused}), 1)
	assert.Equal(t, []string{"volume"}, volumes.detached)
}
