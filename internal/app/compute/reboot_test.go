package compute_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	app "github.com/JSYoo5B/sandstack/internal/app/compute"
	domain "github.com/JSYoo5B/sandstack/internal/domain/compute"
)

func TestRebootAcceptsOnlySupportedStates(t *testing.T) {
	start := time.Date(2026, time.November, 25, 1, 2, 3, 0, time.UTC)
	now := start.Add(time.Minute).In(time.FixedZone("fixture", 3600))
	fault := &domain.Fault{Code: 500, CreatedAt: start}
	for _, test := range []struct {
		name    string
		state   domain.Status
		fault   *domain.Fault
		hard    bool
		wantErr error
	}{
		{name: "soft active"},
		{name: "soft stopped", state: domain.StatusShutoff, wantErr: app.ErrServerPowerState},
		{name: "soft paused", state: domain.StatusPaused, wantErr: app.ErrServerPowerState},
		{name: "soft suspended", state: domain.StatusSuspended, wantErr: app.ErrServerPowerState},
		{name: "soft error", fault: fault, wantErr: app.ErrServerPowerState},
		{name: "hard active", hard: true},
		{name: "hard stopped", state: domain.StatusShutoff, hard: true},
		{name: "hard paused", state: domain.StatusPaused, hard: true},
		{name: "hard suspended", state: domain.StatusSuspended, hard: true},
		{name: "hard error", fault: fault, hard: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			seed := domain.Server{
				ID: "server", ProjectID: "project", UserID: "user", ImageID: "image", Name: "fixture",
				CreatedAt: start, UpdatedAt: start, State: test.state, Fault: test.fault,
			}
			service := app.New(seed, func() time.Time { return now }, seededAttachments{{VolumeID: "volume"}})
			assert.ErrorIs(t, service.Reboot(app.ActionRequest{ProjectID: "foreign"}, seed.ID, test.hard), app.ErrServerNotFound)
			assert.ErrorIs(t, service.Reboot(app.ActionRequest{ProjectID: seed.ProjectID}, "other", test.hard), app.ErrServerNotFound)

			err := service.Reboot(app.ActionRequest{ProjectID: seed.ProjectID}, seed.ID, test.hard)
			assert.ErrorIs(t, err, test.wantErr)
			want := seed
			if test.wantErr == nil {
				want.State = domain.StatusActive
				want.Fault = nil
				want.UpdatedAt = now.UTC()
			}
			current, err := service.Get(seed.ProjectID, seed.ID)
			require.NoError(t, err)
			assert.Equal(t, want, current.Server)
			assert.Equal(t, []string{"volume"}, current.AttachedVolumeIDs)
		})
	}
}

func TestRebootPreservesDetailReadControls(t *testing.T) {
	start := time.Date(2026, time.November, 25, 1, 2, 3, 0, time.UTC)
	seed := domain.Server{ID: "server", ProjectID: "project", State: domain.StatusShutoff, UpdatedAt: start}
	service := app.New(seed, func() time.Time { return start.Add(time.Minute) }, nil)
	require.NoError(t, service.SetStaleDetail(seed.ID, true))
	require.NoError(t, service.SetUnavailableDetail(seed.ID, true))

	require.NoError(t, service.Reboot(app.ActionRequest{ProjectID: seed.ProjectID}, seed.ID, true))
	active := domain.StatusActive
	current := service.List(seed.ProjectID, app.ListFilter{Status: &active})
	require.Len(t, current, 1)
	assert.Equal(t, start.Add(time.Minute), current[0].UpdatedAt)
	_, err := service.Get(seed.ProjectID, seed.ID)
	assert.ErrorIs(t, err, app.ErrDetailUnavailable)
	stale, err := service.Get(seed.ProjectID, seed.ID)
	require.NoError(t, err)
	assert.Equal(t, seed, stale.Server)
	require.NoError(t, service.SetStaleDetail(seed.ID, false))
	currentDetail, err := service.Get(seed.ProjectID, seed.ID)
	require.NoError(t, err)
	assert.Equal(t, current[0], currentDetail)
}
