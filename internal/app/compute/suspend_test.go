package compute_test

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	app "github.com/JSYoo5B/sandstack/internal/app/compute"
	domain "github.com/JSYoo5B/sandstack/internal/domain/compute"
	volumedomain "github.com/JSYoo5B/sandstack/internal/domain/volume"
)

func TestSuspendAndResumeAcceptOnlySupportedStates(t *testing.T) {
	start := time.Date(2026, time.November, 25, 1, 2, 3, 0, time.UTC)
	now := start.Add(time.Minute).In(time.FixedZone("fixture", 3600))
	fault := &domain.Fault{Code: 500, CreatedAt: start}
	for _, test := range []struct {
		name    string
		state   domain.Status
		fault   *domain.Fault
		resume  bool
		want    domain.Status
		wantErr error
	}{
		{name: "suspend active", want: domain.StatusSuspended},
		{name: "suspend suspended", state: domain.StatusSuspended, wantErr: app.ErrServerPowerState},
		{name: "suspend paused", state: domain.StatusPaused, wantErr: app.ErrServerPowerState},
		{name: "suspend stopped", state: domain.StatusShutoff, wantErr: app.ErrServerPowerState},
		{name: "suspend error", fault: fault, wantErr: app.ErrServerPowerState},
		{name: "resume suspended", state: domain.StatusSuspended, resume: true, want: domain.StatusActive},
		{name: "resume active", resume: true, wantErr: app.ErrServerPowerState},
		{name: "resume paused", state: domain.StatusPaused, resume: true, wantErr: app.ErrServerPowerState},
		{name: "resume stopped", state: domain.StatusShutoff, resume: true, wantErr: app.ErrServerPowerState},
		{name: "resume suspended error", state: domain.StatusSuspended, fault: fault, resume: true,
			wantErr: app.ErrServerPowerState},
	} {
		t.Run(test.name, func(t *testing.T) {
			seed := domain.Server{
				ID: "server", ProjectID: "project", UserID: "user", ImageID: "image", Name: "fixture",
				CreatedAt: start, UpdatedAt: start, State: test.state, Fault: test.fault,
			}
			service := app.New(seed, func() time.Time { return now }, seededAttachments{{VolumeID: "volume"}})
			action := service.Suspend
			if test.resume {
				action = service.Resume
			}

			assert.ErrorIs(t, action("foreign", seed.ID), app.ErrServerNotFound)
			assert.ErrorIs(t, action(seed.ProjectID, "other"), app.ErrServerNotFound)
			err := action(seed.ProjectID, seed.ID)

			assert.ErrorIs(t, err, test.wantErr)
			want := seed
			if test.wantErr == nil {
				want.State = test.want
				want.UpdatedAt = now.UTC()
			}
			current, err := service.Get(seed.ProjectID, seed.ID)
			require.NoError(t, err)
			assert.Equal(t, want, current.Server)
			assert.Equal(t, []string{"volume"}, current.AttachedVolumeIDs)
		})
	}
}

func TestSuspendedServerRestrictsPowerAndAttachmentChanges(t *testing.T) {
	start := time.Date(2026, time.November, 25, 1, 2, 3, 0, time.UTC)
	seed := domain.Server{ID: "server", ProjectID: "project", UpdatedAt: start}
	volumes := &recordedAttachments{seededAttachments: seededAttachments{{VolumeID: "volume"}}}
	service := app.New(seed, func() time.Time { return start.Add(time.Minute) }, volumes)
	attachments := app.NewAttachments(service, volumes)
	require.NoError(t, service.Suspend(seed.ProjectID, seed.ID))

	_, attachErr := attachments.Attach(seed.ProjectID, seed.ID, "other", "")
	detachErr := attachments.Detach(seed.ProjectID, seed.ID, "volume")
	missingErr := attachments.Detach(seed.ProjectID, seed.ID, "missing")
	stopErr := service.SetPower(seed.ProjectID, seed.ID, false)
	pauseErr := service.Pause(seed.ProjectID, seed.ID)
	softRebootErr := service.Reboot(seed.ProjectID, seed.ID, false)
	metadataErr := service.PutMetadatum(seed.ProjectID, seed.ID, "item", "value")

	assert.ErrorIs(t, attachErr, app.ErrServerAttachmentState)
	assert.ErrorIs(t, detachErr, app.ErrServerAttachmentState)
	assert.ErrorIs(t, missingErr, errAttachmentMissing)
	assert.ErrorIs(t, stopErr, app.ErrServerPowerState)
	assert.ErrorIs(t, pauseErr, app.ErrServerPowerState)
	assert.ErrorIs(t, softRebootErr, app.ErrServerPowerState)
	assert.NoError(t, metadataErr)
	assert.Empty(t, volumes.attached)
	assert.Empty(t, volumes.detached)

	require.NoError(t, service.Reboot(seed.ProjectID, seed.ID, true))
	require.NoError(t, attachments.Detach(seed.ProjectID, seed.ID, "volume"))

	active := domain.StatusActive
	assert.Len(t, service.List(seed.ProjectID, app.ListFilter{Status: &active}), 1)
	assert.Equal(t, []string{"volume"}, volumes.detached)
}

var errAttachmentMissing = errors.New("attachment missing")

type recordedAttachments struct {
	seededAttachments
	attached []string
	detached []string
}

func (r *recordedAttachments) Attach(_, _, volumeID, _ string) (volumedomain.Attachment, error) {
	r.attached = append(r.attached, volumeID)
	return volumedomain.Attachment{VolumeID: volumeID}, nil
}

func (r *recordedAttachments) GetAttachment(_, _, volumeID string) (volumedomain.Attachment, error) {
	for _, attachment := range r.seededAttachments {
		if attachment.VolumeID == volumeID {
			return attachment, nil
		}
	}
	return volumedomain.Attachment{}, errAttachmentMissing
}

func (r *recordedAttachments) Detach(_, _, volumeID string) error {
	r.detached = append(r.detached, volumeID)
	return nil
}
