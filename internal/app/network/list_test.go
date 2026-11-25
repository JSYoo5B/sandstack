package network_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	app "github.com/JSYoo5B/sandstack/internal/app/network"
	domain "github.com/JSYoo5B/sandstack/internal/domain/network"
)

func TestListFiltersNamesAdministrativeStateAndLiveStatusWithinProject(t *testing.T) {
	seed := domain.Network{ID: fixtureID(1), Name: "fixture", ProjectID: "owner"}
	id := 5
	service := app.New(seed, func() string {
		id--
		return fixtureID(id)
	})
	worker, err := service.Create(seed.ProjectID, "worker", false)
	require.NoError(t, err)
	unnamed, err := service.Create(seed.ProjectID, "", true)
	require.NoError(t, err)
	_, err = service.Create("other-project", worker.Name, true)
	require.NoError(t, err)

	assert.Equal(t, []domain.Network{unnamed}, service.List(seed.ProjectID, app.ListFilter{
		Names: []string{""},
	}))
	assert.Equal(t, []domain.Network{seed, worker}, service.List(seed.ProjectID, app.ListFilter{
		Names:    []string{"worker", "fixture"},
		Statuses: []string{"ACTIVE"},
	}))
	assert.Empty(t, service.List(seed.ProjectID, app.ListFilter{Names: []string{"Worker"}}))
	assert.Empty(t, service.List(seed.ProjectID, app.ListFilter{Statuses: []string{"down"}}))
	assert.Empty(t, service.List(seed.ProjectID, app.ListFilter{Statuses: []string{""}}))

	filter := app.ListFilter{Names: []string{"worker", ""}, Statuses: []string{"DOWN"}}
	assert.Equal(t, []domain.Network{unnamed}, service.List(seed.ProjectID, filter))
	require.NoError(t, service.SetDown(worker.ID, true))
	worker.Down = true
	assert.Equal(t, []domain.Network{unnamed, worker}, service.List(seed.ProjectID, filter))
	filter.AdminStates = []bool{true}
	assert.Equal(t, []domain.Network{worker}, service.List(seed.ProjectID, filter))
	filter.AdminStates = []bool{false}
	assert.Equal(t, []domain.Network{unnamed}, service.List(seed.ProjectID, filter))
	filter.AdminStates = nil
	assert.Equal(t, []domain.Network{seed, unnamed, worker}, service.List(seed.ProjectID, app.ListFilter{
		Names:    []string{},
		Statuses: []string{"DOWN", "ACTIVE"},
	}))

	require.NoError(t, service.SetDown(worker.ID, false))
	assert.Equal(t, []domain.Network{unnamed}, service.List(seed.ProjectID, filter))
}
