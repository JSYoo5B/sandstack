package compute_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	app "github.com/JSYoo5B/sandstack/internal/app/compute"
	domain "github.com/JSYoo5B/sandstack/internal/domain/compute"
	volumedomain "github.com/JSYoo5B/sandstack/internal/domain/volume"
)

func TestStaleDetailKeepsBaselineWhileListObservesLiveState(t *testing.T) {
	start := time.Date(2026, time.November, 25, 1, 2, 3, 0, time.UTC)
	fixture := domain.Server{
		ID:        "server",
		ProjectID: "project",
		CreatedAt: start,
		UpdatedAt: start,
	}
	service := app.New(fixture, func() time.Time { return start.Add(time.Minute) }, nil)

	require.NoError(t, service.SetStaleDetail(fixture.ID, true))
	require.NoError(t, service.SetFault(fixture.ID, true))
	require.NoError(t, service.SetStaleDetail(fixture.ID, true))

	detail, err := service.Get(fixture.ProjectID, fixture.ID)
	require.NoError(t, err)
	assert.Equal(t, fixture, detail.Server)
	_, err = service.Get("other-project", fixture.ID)
	assert.ErrorIs(t, err, app.ErrServerNotFound)
	_, err = service.Get(fixture.ProjectID, "other-server")
	assert.ErrorIs(t, err, app.ErrServerNotFound)
	assert.ErrorIs(t, service.SetStaleDetail("other-server", false), app.ErrServerNotFound)

	active, faulted := domain.StatusActive, domain.StatusError
	assert.Empty(t, service.List(fixture.ProjectID, app.ListFilter{Status: &active}))
	live := service.List(fixture.ProjectID, app.ListFilter{Status: &faulted})
	require.Len(t, live, 1)
	assert.Equal(t, start.Add(time.Minute), live[0].UpdatedAt)

	require.NoError(t, service.SetStaleDetail(fixture.ID, false))
	require.NoError(t, service.SetStaleDetail(fixture.ID, false))
	detail, err = service.Get(fixture.ProjectID, fixture.ID)
	require.NoError(t, err)
	assert.Equal(t, live[0], detail)
}

func TestStaleDetailCopiesCapturedData(t *testing.T) {
	fixture := domain.Server{
		ID:        "server",
		ProjectID: "project",
		Fault:     &domain.Fault{Code: 500},
	}
	attachments := seededAttachments{{VolumeID: "volume"}}
	service := app.New(fixture, time.Now, attachments)

	require.NoError(t, service.SetStaleDetail(fixture.ID, true))
	require.NoError(t, service.SetFault(fixture.ID, false))
	detail, err := service.Get(fixture.ProjectID, fixture.ID)
	require.NoError(t, err)
	require.NotNil(t, detail.Fault)
	require.Len(t, detail.AttachedVolumeIDs, 1)
	assert.Equal(t, fixture, detail.Server)

	detail.Fault.Code = 499
	detail.AttachedVolumeIDs[0] = "changed-by-caller"
	attachments[0].VolumeID = "changed-at-source"
	second, err := service.Get(fixture.ProjectID, fixture.ID)
	require.NoError(t, err)
	assert.Equal(t, fixture, second.Server)
	assert.Equal(t, []string{"volume"}, second.AttachedVolumeIDs)

	require.NoError(t, service.SetStaleDetail(fixture.ID, false))
	require.NoError(t, service.SetStaleDetail(fixture.ID, true))
	detail, err = service.Get(fixture.ProjectID, fixture.ID)
	require.NoError(t, err)
	assert.Nil(t, detail.Fault)
	assert.Equal(t, []string{"changed-at-source"}, detail.AttachedVolumeIDs)
}

type seededAttachments []volumedomain.Attachment

func (attachments seededAttachments) ListAttachments(_, _ string) []volumedomain.Attachment {
	return attachments
}
