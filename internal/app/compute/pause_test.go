package compute_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	app "github.com/JSYoo5B/sandstack/internal/app/compute"
	domain "github.com/JSYoo5B/sandstack/internal/domain/compute"
)

func TestPauseAndUnpauseAcceptOnlySupportedStates(t *testing.T) {
	start := time.Date(2026, time.November, 25, 1, 2, 3, 0, time.UTC)
	now := start.Add(time.Minute).In(time.FixedZone("fixture", 3600))
	fault := &domain.Fault{Code: 500, CreatedAt: start}
	for _, test := range []struct {
		name    string
		state   domain.Status
		fault   *domain.Fault
		unpause bool
		want    domain.Status
		wantErr error
	}{
		{name: "pause active", want: domain.StatusPaused},
		{name: "pause paused", state: domain.StatusPaused, wantErr: app.ErrServerPowerState},
		{name: "pause stopped", state: domain.StatusShutoff, wantErr: app.ErrServerPowerState},
		{name: "pause error", fault: fault, wantErr: app.ErrServerPowerState},
		{name: "unpause paused", state: domain.StatusPaused, unpause: true, want: domain.StatusActive},
		{name: "unpause active", unpause: true, wantErr: app.ErrServerPowerState},
		{name: "unpause stopped", state: domain.StatusShutoff, unpause: true, wantErr: app.ErrServerPowerState},
		{name: "unpause paused error", state: domain.StatusPaused, fault: fault, unpause: true,
			wantErr: app.ErrServerPowerState},
	} {
		t.Run(test.name, func(t *testing.T) {
			seed := domain.Server{
				ID: "server", ProjectID: "project", UserID: "user", ImageID: "image", Name: "fixture",
				CreatedAt: start, UpdatedAt: start, State: test.state, Fault: test.fault,
			}
			service := app.New(seed, func() time.Time { return now }, seededAttachments{{VolumeID: "volume"}})
			action := service.Pause
			if test.unpause {
				action = service.Unpause
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

func TestPausedServerAllowsMetadataWritesAndRejectsStop(t *testing.T) {
	start := time.Date(2026, time.November, 25, 1, 2, 3, 0, time.UTC)
	seed := domain.Server{ID: "server", ProjectID: "project", UpdatedAt: start}
	service := app.New(seed, func() time.Time { return start.Add(time.Minute) }, nil)
	require.NoError(t, service.Pause(seed.ProjectID, seed.ID))

	_, replaceErr := service.ReplaceMetadata(seed.ProjectID, seed.ID, map[string]string{"paused": "yes"})
	putErr := service.PutMetadatum(seed.ProjectID, seed.ID, "item", "value")
	deleteErr := service.DeleteMetadatum(seed.ProjectID, seed.ID, "paused")
	stopErr := service.SetPower(seed.ProjectID, seed.ID, false)
	startErr := service.SetPower(seed.ProjectID, seed.ID, true)
	softRebootErr := service.Reboot(seed.ProjectID, seed.ID, false)

	assert.NoError(t, replaceErr)
	assert.NoError(t, putErr)
	assert.NoError(t, deleteErr)
	assert.ErrorIs(t, stopErr, app.ErrServerPowerState)
	assert.ErrorIs(t, startErr, app.ErrServerPowerState)
	assert.ErrorIs(t, softRebootErr, app.ErrServerPowerState)
	paused := domain.StatusPaused
	current := service.List(seed.ProjectID, app.ListFilter{Status: &paused})
	require.Len(t, current, 1)
	assert.Equal(t, map[string]string{"item": "value"}, current[0].Metadata)
}
