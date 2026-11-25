package compute_test

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	app "github.com/JSYoo5B/sandstack/internal/app/compute"
	domain "github.com/JSYoo5B/sandstack/internal/domain/compute"
)

func TestUnavailableDetailConsumesOnlyOneMatchingRead(t *testing.T) {
	seed := domain.Server{ID: "server", ProjectID: "project"}
	service := app.New(seed, time.Now, nil)
	require.NoError(t, service.SetStaleDetail(seed.ID, true))
	require.NoError(t, service.SetFault(seed.ID, true))
	require.NoError(t, service.SetUnavailableDetail(seed.ID, true))
	require.NoError(t, service.SetUnavailableDetail(seed.ID, true))

	assert.ErrorIs(t, service.SetUnavailableDetail("another-server", false), app.ErrServerNotFound)
	_, err := service.Get(seed.ProjectID, "another-server")
	assert.ErrorIs(t, err, app.ErrServerNotFound)
	_, err = service.Get("another-project", seed.ID)
	assert.ErrorIs(t, err, app.ErrServerNotFound)
	live := service.List(seed.ProjectID, nil)
	require.Len(t, live, 1)
	assert.Equal(t, domain.StatusError, live[0].Status())

	_, err = service.Get(seed.ProjectID, seed.ID)
	assert.ErrorIs(t, err, app.ErrDetailUnavailable)
	stale, err := service.Get(seed.ProjectID, seed.ID)
	require.NoError(t, err)
	assert.Equal(t, seed, stale.Server)
	assert.Equal(t, live, service.List(seed.ProjectID, nil))
}

func TestUnavailableDetailCanBeCanceledAndReset(t *testing.T) {
	seed := domain.Server{ID: "server", ProjectID: "project"}
	service := app.New(seed, time.Now, nil)
	require.NoError(t, service.SetUnavailableDetail(seed.ID, true))
	require.NoError(t, service.SetUnavailableDetail(seed.ID, false))
	require.NoError(t, service.SetUnavailableDetail(seed.ID, false))

	detail, err := service.Get(seed.ProjectID, seed.ID)
	require.NoError(t, err)
	assert.Equal(t, seed, detail.Server)

	require.NoError(t, service.SetUnavailableDetail(seed.ID, true))
	service.Reset()
	detail, err = service.Get(seed.ProjectID, seed.ID)
	require.NoError(t, err)
	assert.Equal(t, seed, detail.Server)
}

func TestConcurrentDetailRequestsConsumeOneOutage(t *testing.T) {
	seed := domain.Server{ID: "server", ProjectID: "project"}
	service := app.New(seed, time.Now, nil)
	require.NoError(t, service.SetUnavailableDetail(seed.ID, true))

	const attempts = 20
	results := make(chan error, attempts)
	var wait sync.WaitGroup
	for range attempts {
		wait.Go(func() {
			_, err := service.Get(seed.ProjectID, seed.ID)
			results <- err
		})
	}
	wait.Wait()
	close(results)

	outages := 0
	for err := range results {
		if errors.Is(err, app.ErrDetailUnavailable) {
			outages++
		} else {
			assert.NoError(t, err)
		}
	}
	assert.Equal(t, 1, outages)
}
