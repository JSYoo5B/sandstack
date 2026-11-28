package compute_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	app "github.com/JSYoo5B/sandstack/internal/app/compute"
	domain "github.com/JSYoo5B/sandstack/internal/domain/compute"
)

// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L2870-L2899
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/db/main/api.py#L3896-L3937
func TestSuccessfulActionsRecordHistoryNewestFirst(t *testing.T) {
	start := time.Date(2026, time.November, 25, 1, 2, 3, 0, time.UTC)
	now := start.In(time.FixedZone("fixture", 3600))
	seed := domain.Server{ID: "server", ProjectID: "project", UserID: "owner", CreatedAt: start, UpdatedAt: start}
	created := domain.Action{
		Name: domain.ActionCreate, ServerID: seed.ID, RequestID: "req-create",
		ProjectID: seed.ProjectID, UserID: seed.UserID, StartTime: start,
	}
	volumes := &recordedAttachments{seededAttachments: seededAttachments{{VolumeID: "volume"}}}
	service := app.New(seed, func() time.Time { return now }, volumes, created)
	attachments := app.NewAttachments(service, volumes)
	request := func(id string) app.ActionRequest {
		return app.ActionRequest{ProjectID: seed.ProjectID, UserID: "caller", RequestID: id}
	}
	record := func(name, id string, minutes int) domain.Action {
		return domain.Action{
			Name: name, ServerID: seed.ID, RequestID: id, ProjectID: seed.ProjectID,
			UserID: "caller", StartTime: start.Add(time.Duration(minutes) * time.Minute),
		}
	}
	steps := []func() error{
		func() error { return service.SetPower(request("req-stop"), seed.ID, false) },
		func() error { return service.SetPower(request("req-start"), seed.ID, true) },
		func() error { return service.Reboot(request("req-reboot"), seed.ID, true) },
		func() error { return service.Pause(request("req-pause"), seed.ID) },
		func() error { return service.Unpause(request("req-unpause"), seed.ID) },
		func() error { return service.Suspend(request("req-suspend"), seed.ID) },
		func() error { return service.Resume(request("req-resume"), seed.ID) },
		func() error {
			_, err := attachments.Attach(request("req-attach"), seed.ID, "other", "")
			return err
		},
		func() error { return attachments.Detach(request("req-detach"), seed.ID, "volume") },
		func() error { return service.Lock(request("req-lock"), seed.ID) },
		func() error { return service.Unlock(request("req-unlock"), seed.ID) },
	}

	for _, step := range steps {
		now = now.Add(time.Minute)
		require.NoError(t, step())
	}
	actions, listErr := service.ListActions(seed.ProjectID, seed.ID)
	reboot, getErr := service.GetAction(seed.ProjectID, seed.ID, "req-reboot")

	require.NoError(t, listErr)
	require.NoError(t, getErr)
	assert.Equal(t, []domain.Action{
		record(domain.ActionUnlock, "req-unlock", 11),
		record(domain.ActionLock, "req-lock", 10),
		record(domain.ActionDetachVolume, "req-detach", 9),
		record(domain.ActionAttachVolume, "req-attach", 8),
		record(domain.ActionResume, "req-resume", 7),
		record(domain.ActionSuspend, "req-suspend", 6),
		record(domain.ActionUnpause, "req-unpause", 5),
		record(domain.ActionPause, "req-pause", 4),
		record(domain.ActionReboot, "req-reboot", 3),
		record(domain.ActionStart, "req-start", 2),
		record(domain.ActionStop, "req-stop", 1),
		created,
	}, actions)
	assert.Equal(t, record(domain.ActionReboot, "req-reboot", 3), reboot)
}

// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L5046-L5055
// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L4852-L4868
func TestRejectedActionsLeaveHistoryUnchanged(t *testing.T) {
	start := time.Date(2026, time.November, 25, 1, 2, 3, 0, time.UTC)
	seed := domain.Server{ID: "server", ProjectID: "project", UpdatedAt: start}
	volumes := &recordedAttachments{seededAttachments: seededAttachments{{VolumeID: "volume"}}}
	service := app.New(seed, func() time.Time { return start.Add(time.Minute) }, volumes)
	attachments := app.NewAttachments(service, volumes)
	owner := app.ActionRequest{ProjectID: seed.ProjectID, UserID: "user", RequestID: "req-lock"}
	require.NoError(t, service.Lock(owner, seed.ID))
	repeat := owner
	repeat.RequestID = "req-repeat"

	repeatErr := service.Lock(repeat, seed.ID)
	stopErr := service.SetPower(repeat, seed.ID, false)
	_, attachErr := attachments.Attach(repeat, seed.ID, "other", "")
	unpauseErr := service.Unpause(app.ActionRequest{ProjectID: seed.ProjectID}, seed.ID)
	foreignErr := service.Pause(app.ActionRequest{ProjectID: "foreign", RequestID: "req-foreign"}, seed.ID)
	actions, err := service.ListActions(seed.ProjectID, seed.ID)

	require.NoError(t, err)
	assert.NoError(t, repeatErr)
	assert.ErrorIs(t, stopErr, app.ErrServerLocked)
	assert.ErrorIs(t, attachErr, app.ErrServerLocked)
	assert.ErrorIs(t, unpauseErr, app.ErrServerLocked)
	assert.ErrorIs(t, foreignErr, app.ErrServerNotFound)
	require.Len(t, actions, 1)
	assert.Equal(t, "req-lock", actions[0].RequestID)
	_, err = service.GetAction(seed.ProjectID, seed.ID, "req-repeat")
	assert.ErrorIs(t, err, app.ErrActionNotFound)
}

func TestActionHistoryLookupAndReset(t *testing.T) {
	start := time.Date(2026, time.November, 25, 1, 2, 3, 0, time.UTC)
	seed := domain.Server{ID: "server", ProjectID: "project", UpdatedAt: start}
	created := domain.Action{Name: domain.ActionCreate, ServerID: seed.ID, RequestID: "req-create"}
	service := app.New(seed, func() time.Time { return start }, nil, created)
	require.NoError(t, service.Pause(app.ActionRequest{ProjectID: seed.ProjectID, RequestID: "req-pause"}, seed.ID))

	_, foreignListErr := service.ListActions("foreign", seed.ID)
	_, otherServerErr := service.GetAction(seed.ProjectID, "other", "req-pause")
	_, missingErr := service.GetAction(seed.ProjectID, seed.ID, "req-missing")
	service.Reset()
	actions, listErr := service.ListActions(seed.ProjectID, seed.ID)
	_, pausedErr := service.GetAction(seed.ProjectID, seed.ID, "req-pause")

	assert.ErrorIs(t, foreignListErr, app.ErrServerNotFound)
	assert.ErrorIs(t, otherServerErr, app.ErrServerNotFound)
	assert.ErrorIs(t, missingErr, app.ErrActionNotFound)
	require.NoError(t, listErr)
	assert.Equal(t, []domain.Action{created}, actions)
	assert.ErrorIs(t, pausedErr, app.ErrActionNotFound)
}
