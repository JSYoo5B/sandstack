package volume_test

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	app "github.com/JSYoo5B/sandstack/internal/app/volume"
	domain "github.com/JSYoo5B/sandstack/internal/domain/volume"
)

func TestConcurrentVolumeDetailReadsConsumeOneFailure(t *testing.T) {
	seed := domain.Volume{ID: fixtureID, ProjectID: "project", State: domain.StateAvailable}
	service := app.New(seed, func() time.Time { return time.Time{} }, nil)
	require.NoError(t, service.SetUnavailableDetail(seed.ID, true))
	require.NoError(t, service.SetUnavailableDetail(seed.ID, true))

	const attempts = 16
	start := make(chan struct{})
	type result struct {
		volume domain.Volume
		err    error
	}
	results := make(chan result, attempts)
	for range attempts {
		go func() {
			<-start
			volume, err := service.Get(seed.ProjectID, seed.ID)
			results <- result{volume: volume, err: err}
		}()
	}
	close(start)

	failures := 0
	for range attempts {
		response := <-results
		if errors.Is(response.err, app.ErrDetailUnavailable) {
			failures++
			continue
		}
		if assert.NoError(t, response.err) {
			assert.Equal(t, seed, response.volume)
		}
	}
	assert.Equal(t, 1, failures)
}

func TestDeletingVolumeDiscardsPendingDetailFailure(t *testing.T) {
	seed := domain.Volume{ID: fixtureID, ProjectID: "project"}
	const volumeID = "8593c006-d628-4ebe-bc2f-2ccd86f27d9d"
	service := app.New(seed, func() time.Time { return time.Time{} }, func() string { return volumeID })
	oldVolume, err := service.Create(seed.ProjectID, seed.UserID, "old", "", 1)
	require.NoError(t, err)
	require.NoError(t, service.CompleteCreation(oldVolume.ID))
	require.NoError(t, service.SetUnavailableDetail(oldVolume.ID, true))
	require.NoError(t, service.Delete(seed.ProjectID, oldVolume.ID))
	replacement, err := service.Create(seed.ProjectID, seed.UserID, "replacement", "", 1)
	require.NoError(t, err)
	require.Equal(t, oldVolume.ID, replacement.ID)

	got, err := service.Get(seed.ProjectID, replacement.ID)
	require.NoError(t, err)
	assert.Equal(t, replacement, got)
}
