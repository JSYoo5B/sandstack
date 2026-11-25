package network_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	app "github.com/JSYoo5B/sandstack/internal/app/network"
	domain "github.com/JSYoo5B/sandstack/internal/domain/network"
)

func TestScopedRouterLifecycleUsesSharedNetworkIDs(t *testing.T) {
	seed := domain.Network{ID: fixtureID(1), ProjectID: "owner"}
	ids := []int{1, 8, 8, 4, 3, 5}
	service := app.New(seed, func() string {
		id := ids[0]
		ids = ids[1:]
		return fixtureID(id)
	})
	assert.Equal(t, []domain.Router{}, service.ListRouters(seed.ProjectID, app.RouterListFilter{}))

	first, err := service.CreateRouter(seed.ProjectID, "private router", "description", false)
	require.NoError(t, err)
	assert.Equal(t, domain.Router{
		ID: fixtureID(8), ProjectID: seed.ProjectID, Name: "private router", Description: "description",
	}, first)
	network, err := service.Create(seed.ProjectID, "network", false)
	require.NoError(t, err)
	assert.Equal(t, fixtureID(4), network.ID)
	second, err := service.CreateRouter(seed.ProjectID, "", "", true)
	require.NoError(t, err)
	assert.Equal(t, fixtureID(3), second.ID)
	assert.True(t, second.AdminStateUp)
	foreign, err := service.CreateRouter("other-project", "", "", true)
	require.NoError(t, err)

	assert.Equal(t, []domain.Router{second, first}, service.ListRouters(seed.ProjectID, app.RouterListFilter{}))
	assert.Equal(t, []domain.Router{foreign}, service.ListRouters(foreign.ProjectID, app.RouterListFilter{}))
	filter := app.RouterListFilter{Names: []string{""}, AdminStates: []bool{true}}
	assert.Equal(t, []domain.Router{second}, service.ListRouters(seed.ProjectID, filter))
	_, err = service.ListRoutersPage(seed.ProjectID, filter, app.PageOptions{Limit: 1, Marker: foreign.ID})
	assert.ErrorIs(t, err, app.ErrRouterNotFound)
	page, err := service.ListRoutersPage(seed.ProjectID, filter, app.PageOptions{Limit: 1, Marker: first.ID})
	require.NoError(t, err)
	assert.Empty(t, page.Items)
	_, err = service.GetRouter(seed.ProjectID, foreign.ID)
	assert.ErrorIs(t, err, app.ErrRouterNotFound)
	foreignName := "unauthorized name"
	_, err = service.UpdateRouter(seed.ProjectID, foreign.ID, app.RouterUpdate{Name: &foreignName})
	assert.ErrorIs(t, err, app.ErrRouterNotFound)
	assert.Equal(t, []domain.Router{foreign}, service.ListRouters(foreign.ProjectID, app.RouterListFilter{}))
	assert.ErrorIs(t, service.DeleteRouter(seed.ProjectID, foreign.ID), app.ErrRouterNotFound)
	loaded, err := service.GetRouter(seed.ProjectID, first.ID)
	require.NoError(t, err)
	assert.Equal(t, first, loaded)

	require.NoError(t, service.DeleteRouter(seed.ProjectID, first.ID))
	_, err = service.GetRouter(seed.ProjectID, first.ID)
	assert.ErrorIs(t, err, app.ErrRouterNotFound)
	assert.ErrorIs(t, service.DeleteRouter(seed.ProjectID, first.ID), app.ErrRouterNotFound)
	assert.Equal(t, []domain.Router{second}, service.ListRouters(seed.ProjectID, app.RouterListFilter{}))

	service.Reset()
	assert.Empty(t, service.ListRouters(seed.ProjectID, app.RouterListFilter{}))
	assert.Empty(t, service.ListRouters(foreign.ProjectID, app.RouterListFilter{}))
	assert.Equal(t, []domain.Network{seed}, service.List(seed.ProjectID, app.ListFilter{}))
}
