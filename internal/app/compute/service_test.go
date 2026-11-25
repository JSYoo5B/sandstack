package compute_test

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	app "github.com/JSYoo5B/sandstack/internal/app/compute"
	domain "github.com/JSYoo5B/sandstack/internal/domain/compute"
)

func TestSetFaultTimestampsChangeOnlyOnTransition(t *testing.T) {
	start := time.Date(2026, time.November, 25, 1, 2, 3, 0, time.UTC)
	now := start.In(time.FixedZone("fixture", 3600))
	fixture := domain.Server{
		ID:        "server",
		ProjectID: "project",
		CreatedAt: start,
		UpdatedAt: start,
	}
	service := app.New(fixture, func() time.Time { return now }, nil)

	now = now.Add(time.Minute)
	require.NoError(t, service.SetFault(fixture.ID, true))
	faulted, err := service.Get(fixture.ProjectID, fixture.ID)
	require.NoError(t, err)
	assert.Equal(t, start.Add(time.Minute), faulted.UpdatedAt)
	assert.Equal(t, &domain.Fault{
		Code:      500,
		Message:   "Injected server fault",
		CreatedAt: start.Add(time.Minute),
	}, faulted.Fault)

	now = now.Add(time.Minute)
	require.NoError(t, service.SetFault(fixture.ID, true))
	retried, err := service.Get(fixture.ProjectID, fixture.ID)
	require.NoError(t, err)
	assert.Equal(t, faulted, retried)

	require.NoError(t, service.SetFault(fixture.ID, false))
	cleared, err := service.Get(fixture.ProjectID, fixture.ID)
	require.NoError(t, err)
	assert.Nil(t, cleared.Fault)
	assert.Equal(t, start, cleared.CreatedAt)
	assert.Equal(t, start.Add(2*time.Minute), cleared.UpdatedAt)

	now = now.Add(time.Minute)
	require.NoError(t, service.SetFault(fixture.ID, false))
	retried, err = service.Get(fixture.ProjectID, fixture.ID)
	require.NoError(t, err)
	assert.Equal(t, cleared, retried)
}

func TestServerSnapshotsOwnTheirFaultData(t *testing.T) {
	fixture := domain.Server{
		ID:        "server",
		ProjectID: "project",
		Fault:     &domain.Fault{Code: 500},
	}
	service := app.New(fixture, time.Now, nil)

	fixture.Fault.Code = 499
	first, err := service.Get(fixture.ProjectID, fixture.ID)
	require.NoError(t, err)
	require.NotNil(t, first.Fault)
	assert.Equal(t, 500, first.Fault.Code)

	first.Fault.Code = 498
	second, err := service.Get(fixture.ProjectID, fixture.ID)
	require.NoError(t, err)
	require.NotNil(t, second.Fault)
	assert.Equal(t, 500, second.Fault.Code)
}

func TestGetRequiresSeededServerAndProject(t *testing.T) {
	fixture := domain.Server{ID: "server", ProjectID: "project"}
	service := app.New(fixture, time.Now, nil)

	_, err := service.Get("other-project", fixture.ID)
	assert.ErrorIs(t, err, app.ErrServerNotFound)
	_, err = service.Get(fixture.ProjectID, "other-server")
	assert.ErrorIs(t, err, app.ErrServerNotFound)
	assert.ErrorIs(t, service.SetFault("other-server", true), app.ErrServerNotFound)

	server, err := service.Get(fixture.ProjectID, fixture.ID)
	require.NoError(t, err)
	assert.Equal(t, fixture, server.Server)
}

func TestConcurrentFaultChangesAndSnapshots(t *testing.T) {
	start := time.Date(2026, time.November, 25, 1, 2, 3, 0, time.UTC)
	now := start
	fixture := domain.Server{
		ID:        "server",
		ProjectID: "project",
		CreatedAt: start,
		UpdatedAt: start,
	}
	service := app.New(fixture, func() time.Time {
		now = now.Add(time.Second)
		return now
	}, nil)

	var wait sync.WaitGroup
	for i := range 40 {
		wait.Go(func() {
			assert.NoError(t, service.SetFault(fixture.ID, i%2 == 0))
			server, err := service.Get(fixture.ProjectID, fixture.ID)
			if assert.NoError(t, err) && server.Fault != nil {
				assert.Equal(t, server.UpdatedAt, server.Fault.CreatedAt)
			}
			servers := service.List(fixture.ProjectID, app.ListFilter{})
			if assert.Len(t, servers, 1) && servers[0].Fault != nil {
				assert.Equal(t, servers[0].UpdatedAt, servers[0].Fault.CreatedAt)
			}
		})
	}
	wait.Wait()
}
