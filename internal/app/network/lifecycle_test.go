package network_test

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	app "github.com/JSYoo5B/sandstack/internal/app/network"
	domain "github.com/JSYoo5B/sandstack/internal/domain/network"
)

func TestNetworkLifecycleKeepsProjectsAndOperationalFaultsSeparate(t *testing.T) {
	seed := domain.Network{ID: fixtureID(3), Name: "fixture", ProjectID: "owner", Down: true}
	id := 6
	service := app.New(seed, func() string {
		id -= 2
		return fixtureID(id)
	})

	other, err := service.Create("another-project", "other", false)
	require.NoError(t, err)
	created, err := service.Create(seed.ProjectID, "new network", true)
	require.NoError(t, err)
	assert.True(t, created.AdminDown)
	assert.False(t, created.Down)
	assert.Equal(t, []domain.Network{created, seed}, service.List(seed.ProjectID, app.ListFilter{}))
	assert.Equal(t, []domain.Network{other}, service.List(other.ProjectID, app.ListFilter{}))

	_, err = service.Get(other.ProjectID, created.ID)
	assert.ErrorIs(t, err, app.ErrNetworkNotFound)
	assert.ErrorIs(t, service.Delete(other.ProjectID, created.ID), app.ErrNetworkNotFound)
	require.NoError(t, service.SetDown(created.ID, true))
	faulted, err := service.Get(seed.ProjectID, created.ID)
	require.NoError(t, err)
	assert.True(t, faulted.Down)
	assert.True(t, faulted.AdminDown)

	require.NoError(t, service.Delete(seed.ProjectID, created.ID))
	assert.ErrorIs(t, service.SetDown(created.ID, false), app.ErrNetworkNotFound)
	assert.ErrorIs(t, service.Delete(seed.ProjectID, created.ID), app.ErrNetworkNotFound)
	require.NoError(t, service.Delete(seed.ProjectID, seed.ID))
	assert.Empty(t, service.List(seed.ProjectID, app.ListFilter{}))

	service.Reset()
	assert.Equal(t, []domain.Network{seed}, service.List(seed.ProjectID, app.ListFilter{}))
	assert.Empty(t, service.List(other.ProjectID, app.ListFilter{}))
}

func TestCreateRetriesReservedSeedIDAfterDeletion(t *testing.T) {
	seed := domain.Network{ID: fixtureID(1), ProjectID: "owner"}
	id := 0
	service := app.New(seed, func() string {
		id++
		return fixtureID(id)
	})
	require.NoError(t, service.Delete(seed.ProjectID, seed.ID))

	network, err := service.Create(seed.ProjectID, "created", false)

	require.NoError(t, err)
	assert.Equal(t, fixtureID(2), network.ID)
}

func TestCreateRejectsCollisionsWithoutOverwriting(t *testing.T) {
	seed := domain.Network{ID: fixtureID(1), ProjectID: "owner"}
	attempts := 0
	service := app.New(seed, func() string {
		attempts++
		if attempts > 100 {
			return fixtureID(3)
		}
		return fixtureID(2)
	})
	first, err := service.Create(seed.ProjectID, "original", false)
	require.NoError(t, err)

	_, err = service.Create(seed.ProjectID, "replacement", true)

	assert.ErrorIs(t, err, app.ErrNetworkIDGeneration)
	assert.Equal(t, []domain.Network{seed, first}, service.List(seed.ProjectID, app.ListFilter{}))
}

func TestCreateRejectsInvalidGeneratorOutputs(t *testing.T) {
	seed := domain.Network{ID: fixtureID(1), ProjectID: "owner"}
	for _, generate := range []func() string{
		nil,
		func() string { return "" },
		func() string { return "not-a-uuid" },
	} {
		service := app.New(seed, generate)

		_, err := service.Create(seed.ProjectID, "created", false)

		assert.ErrorIs(t, err, app.ErrNetworkIDGeneration)
		assert.Equal(t, []domain.Network{seed}, service.List(seed.ProjectID, app.ListFilter{}))
	}
}

func TestConcurrentCreatesProduceUniqueNetworks(t *testing.T) {
	seed := domain.Network{ID: fixtureID(1), ProjectID: "owner"}
	id := 1
	service := app.New(seed, func() string {
		id++
		return fixtureID(id)
	})

	const attempts = 20
	var wait sync.WaitGroup
	for range attempts {
		wait.Go(func() {
			created, err := service.Create(seed.ProjectID, "created", false)
			if assert.NoError(t, err) {
				assert.Contains(t, service.List(seed.ProjectID, app.ListFilter{}), created)
			}
		})
	}
	wait.Wait()

	assert.Len(t, service.List(seed.ProjectID, app.ListFilter{}), attempts+1)
}

func fixtureID(id int) string {
	return fmt.Sprintf("%08x-0000-4000-8000-000000000000", id)
}
