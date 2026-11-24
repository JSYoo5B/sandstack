package compute_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	app "github.com/JSYoo5B/sandstack/internal/app/compute"
	domain "github.com/JSYoo5B/sandstack/internal/domain/compute"
)

func TestStaleDetailKeepsBaselineWhileListObservesLiveState(t *testing.T) {
	start := time.Date(2026, time.November, 25, 1, 2, 3, 0, time.UTC)
	fixture := domain.Server{
		ID:        "server",
		ProjectID: "project",
		CreatedAt: start,
		UpdatedAt: start,
	}
	service := app.New(fixture, func() time.Time { return start.Add(time.Minute) })

	require.NoError(t, service.SetStaleDetail(fixture.ID, true))
	require.NoError(t, service.SetFault(fixture.ID, true))
	require.NoError(t, service.SetStaleDetail(fixture.ID, true))

	detail, err := service.Get(fixture.ProjectID, fixture.ID)
	require.NoError(t, err)
	assert.Equal(t, fixture, detail)
	_, err = service.Get("other-project", fixture.ID)
	assert.ErrorIs(t, err, app.ErrServerNotFound)
	_, err = service.Get(fixture.ProjectID, "other-server")
	assert.ErrorIs(t, err, app.ErrServerNotFound)
	assert.ErrorIs(t, service.SetStaleDetail("other-server", false), app.ErrServerNotFound)

	active, faulted := domain.StatusActive, domain.StatusError
	assert.Empty(t, service.List(fixture.ProjectID, &active))
	live := service.List(fixture.ProjectID, &faulted)
	require.Len(t, live, 1)
	assert.Equal(t, start.Add(time.Minute), live[0].UpdatedAt)

	require.NoError(t, service.SetStaleDetail(fixture.ID, false))
	require.NoError(t, service.SetStaleDetail(fixture.ID, false))
	detail, err = service.Get(fixture.ProjectID, fixture.ID)
	require.NoError(t, err)
	assert.Equal(t, live[0], detail)
}

func TestStaleDetailCopiesCapturedFaultData(t *testing.T) {
	fixture := domain.Server{
		ID:        "server",
		ProjectID: "project",
		Fault:     &domain.Fault{Code: 500},
	}
	service := app.New(fixture, time.Now)

	require.NoError(t, service.SetStaleDetail(fixture.ID, true))
	require.NoError(t, service.SetFault(fixture.ID, false))
	detail, err := service.Get(fixture.ProjectID, fixture.ID)
	require.NoError(t, err)
	require.NotNil(t, detail.Fault)
	assert.Equal(t, fixture, detail)

	detail.Fault.Code = 499
	second, err := service.Get(fixture.ProjectID, fixture.ID)
	require.NoError(t, err)
	assert.Equal(t, fixture, second)

	require.NoError(t, service.SetStaleDetail(fixture.ID, false))
	require.NoError(t, service.SetStaleDetail(fixture.ID, true))
	detail, err = service.Get(fixture.ProjectID, fixture.ID)
	require.NoError(t, err)
	assert.Nil(t, detail.Fault)
}
