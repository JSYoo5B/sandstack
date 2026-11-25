package compute_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	app "github.com/JSYoo5B/sandstack/internal/app/compute"
	domain "github.com/JSYoo5B/sandstack/internal/domain/compute"
)

func TestResetRestoresSeedAndClearsStaleDetail(t *testing.T) {
	start := time.Date(2026, time.November, 25, 1, 2, 3, 0, time.UTC)
	seedFault := domain.Fault{Code: 502, Message: "seed fault", CreatedAt: start}
	seed := domain.Server{
		ID:        "server",
		Name:      "seed server",
		ProjectID: "project",
		UserID:    "user",
		CreatedAt: start.Add(-time.Minute),
		UpdatedAt: start,
		Fault:     &seedFault,
	}
	service := app.New(seed, func() time.Time { return start.Add(time.Minute) }, nil)
	expected := seed
	expectedFault := seedFault
	expected.Fault = &expectedFault
	seedFault.Code = 499
	require.NoError(t, service.SetFault(seed.ID, false))
	require.NoError(t, service.SetStaleDetail(seed.ID, true))
	require.NoError(t, service.SetFault(seed.ID, true))

	service.Reset()
	detail, err := service.Get(seed.ProjectID, seed.ID)
	require.NoError(t, err)
	assert.Equal(t, expected, detail.Server)
	assert.Equal(t, []app.ServerDetail{{Server: expected}}, service.List(seed.ProjectID, app.ListFilter{}))

	require.NotNil(t, detail.Fault)
	detail.Fault.Code = 400
	service.Reset()
	service.Reset()
	detail, err = service.Get(seed.ProjectID, seed.ID)
	require.NoError(t, err)
	assert.Equal(t, expected, detail.Server)
}
