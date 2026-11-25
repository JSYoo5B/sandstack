package compute_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	app "github.com/JSYoo5B/sandstack/internal/app/compute"
	domain "github.com/JSYoo5B/sandstack/internal/domain/compute"
)

func TestListMatchesProjectAndCurrentStatus(t *testing.T) {
	fixture := domain.Server{ID: "server", ProjectID: "project"}
	service := app.New(fixture, time.Now, nil)
	active, faulted := domain.StatusActive, domain.StatusError
	empty, unknown := domain.Status(""), domain.Status("unsupported")

	assert.Equal(t, []app.ServerDetail{{Server: fixture}}, service.List(fixture.ProjectID, nil))
	assert.Equal(t, []app.ServerDetail{{Server: fixture}}, service.List(fixture.ProjectID, &active))
	assert.Empty(t, service.List(fixture.ProjectID, &faulted))
	assert.Empty(t, service.List(fixture.ProjectID, &empty))
	assert.Empty(t, service.List(fixture.ProjectID, &unknown))
	assert.Empty(t, service.List("other-project", nil))
	assert.Empty(t, service.List("other-project", &active))

	require.NoError(t, service.SetFault(fixture.ID, true))
	expected, err := service.Get(fixture.ProjectID, fixture.ID)
	require.NoError(t, err)
	assert.Equal(t, []app.ServerDetail{expected}, service.List(fixture.ProjectID, nil))
	assert.Equal(t, []app.ServerDetail{expected}, service.List(fixture.ProjectID, &faulted))
	assert.Empty(t, service.List(fixture.ProjectID, &active))

	require.NoError(t, service.SetFault(fixture.ID, false))
	restored := service.List(fixture.ProjectID, &active)
	require.Len(t, restored, 1)
	assert.Equal(t, fixture.ID, restored[0].ID)
	assert.Nil(t, restored[0].Fault)
	assert.Empty(t, service.List(fixture.ProjectID, &faulted))
}

func TestListCopiesFaultDataOutsideService(t *testing.T) {
	fixture := domain.Server{
		ID:        "server",
		ProjectID: "project",
		Fault:     &domain.Fault{Code: 500},
	}
	service := app.New(fixture, time.Now, nil)
	servers := service.List(fixture.ProjectID, nil)
	require.Len(t, servers, 1)
	require.NotNil(t, servers[0].Fault)

	servers[0].Fault.Code = 499
	servers[0].ID = "mutated"
	server, err := service.Get(fixture.ProjectID, fixture.ID)

	require.NoError(t, err)
	assert.Equal(t, fixture, server.Server)
}
