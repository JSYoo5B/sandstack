package network_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	app "github.com/JSYoo5B/sandstack/internal/app/network"
	domain "github.com/JSYoo5B/sandstack/internal/domain/network"
)

func TestPrivateNetworkIsVisibleOnlyToOwner(t *testing.T) {
	seed := domain.Network{ID: "net", Name: "fixture", ProjectID: "owner"}
	service := app.New(seed)

	assert.Empty(t, service.List("other-project"))
	_, err := service.Get("other-project", seed.ID)
	assert.ErrorIs(t, err, app.ErrNetworkNotFound)

	assert.Equal(t, []domain.Network{seed}, service.List(seed.ProjectID))
	network, err := service.Get(seed.ProjectID, seed.ID)
	require.NoError(t, err)
	assert.Equal(t, seed, network)
}

func TestResetRestoresSeededOperationalState(t *testing.T) {
	seed := domain.Network{ID: "net", Name: "fixture", ProjectID: "owner", Down: true}
	service := app.New(seed)

	assert.ErrorIs(t, service.SetDown("other-network", false), app.ErrNetworkNotFound)
	assert.Equal(t, []domain.Network{seed}, service.List(seed.ProjectID))

	require.NoError(t, service.SetDown(seed.ID, false))
	up, err := service.Get(seed.ProjectID, seed.ID)
	require.NoError(t, err)
	assert.False(t, up.Down)

	service.Reset()

	assert.Equal(t, []domain.Network{seed}, service.List(seed.ProjectID))
}
