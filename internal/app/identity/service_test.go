package identity_test

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	app "github.com/JSYoo5B/sandstack/internal/app/identity"
	domain "github.com/JSYoo5B/sandstack/internal/domain/identity"
	store "github.com/JSYoo5B/sandstack/internal/store/memory/identity"
)

func TestAuthenticateSelectors(t *testing.T) {
	fixture, credentials := testIdentity()
	expiresIn := time.Hour
	now := time.Date(2026, time.November, 25, 0, 0, 0, 0, time.UTC)
	service := app.New(fixture, "password", expiresIn,
		func() time.Time { return now }, func() string { return "entropy" }, store.New(), store.NewApplicationCredentials(), nil,
	)

	tests := map[string]domain.Credentials{
		"IDs": credentials,
		"names and domain IDs": {
			User:     domain.Selector{Name: fixture.User.Name, DomainID: fixture.User.Domain.ID},
			Project:  domain.Selector{Name: fixture.Project.Name, DomainID: fixture.Project.Domain.ID},
			Password: credentials.Password,
		},
		"names and domain names": {
			User:     domain.Selector{Name: fixture.User.Name, DomainName: fixture.User.Domain.Name},
			Project:  domain.Selector{Name: fixture.Project.Name, DomainName: fixture.Project.Domain.Name},
			Password: credentials.Password,
		},
	}
	for name, auth := range tests {
		t.Run(name, func(t *testing.T) {
			token, err := service.Authenticate(auth)

			require.NoError(t, err)
			assert.NotEmpty(t, token.ID)
			assert.Equal(t, "entropy", token.AuditID)
			assert.Equal(t, fixture, token.Fixture)
			assert.Equal(t, now, token.IssuedAt)
			assert.Equal(t, now.Add(expiresIn), token.ExpiresAt)
			assert.NoError(t, service.Validate(token.ID))
		})
	}
}

func TestAuthenticateRejectsInvalidCredentials(t *testing.T) {
	fixture, credentials := testIdentity()
	service := app.New(fixture, "password", time.Hour, time.Now,
		func() string {
			t.Fatal("invalid credentials reached token generation")
			return ""
		},
		store.New(), store.NewApplicationCredentials(), nil,
	)

	tests := map[string]domain.Credentials{
		"wrong password": {
			User:     credentials.User,
			Project:  credentials.Project,
			Password: "wrong",
		},
		"missing password": {
			User:    credentials.User,
			Project: credentials.Project,
		},
		"unknown user": {
			User:     domain.Selector{ID: "unknown"},
			Project:  credentials.Project,
			Password: credentials.Password,
		},
		"missing project": {
			User:     credentials.User,
			Password: credentials.Password,
		},
		"ambiguous user": {
			User:     domain.Selector{ID: fixture.User.ID, Name: fixture.User.Name},
			Project:  credentials.Project,
			Password: credentials.Password,
		},
		"missing user domain": {
			User:     domain.Selector{Name: fixture.User.Name},
			Project:  credentials.Project,
			Password: credentials.Password,
		},
		"wrong project domain": {
			User:     credentials.User,
			Project:  domain.Selector{Name: fixture.Project.Name, DomainID: fixture.User.Domain.ID},
			Password: credentials.Password,
		},
		"ambiguous domain": {
			User: credentials.User,
			Project: domain.Selector{
				Name:       fixture.Project.Name,
				DomainID:   fixture.Project.Domain.ID,
				DomainName: fixture.Project.Domain.Name,
			},
			Password: credentials.Password,
		},
	}
	for name, auth := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := service.Authenticate(auth)

			assert.ErrorIs(t, err, app.ErrInvalidCredentials)
		})
	}
}

func TestTokenExpiresAtTTLBoundary(t *testing.T) {
	fixture, credentials := testIdentity()
	now := time.Date(2026, time.November, 25, 0, 0, 0, 0, time.UTC)
	service := app.New(fixture, "password", time.Hour,
		func() time.Time { return now }, func() string { return "entropy" }, store.New(), store.NewApplicationCredentials(), nil,
	)
	token, err := service.Authenticate(credentials)
	require.NoError(t, err)

	now = token.ExpiresAt.Add(-time.Nanosecond)
	assert.NoError(t, service.Validate(token.ID))
	userID, err := service.UserForToken(token.ID)
	assert.NoError(t, err)
	assert.Equal(t, fixture.User.ID, userID)

	now = token.ExpiresAt
	assert.ErrorIs(t, service.Validate(token.ID), app.ErrInvalidToken)
	_, err = service.UserForToken(token.ID)
	assert.ErrorIs(t, err, app.ErrInvalidToken)
}

func TestInvalidateTokensDoesNotReuseTokenIDs(t *testing.T) {
	fixture, credentials := testIdentity()
	service := app.New(fixture, "password", time.Hour, time.Now,
		func() string { return "repeated entropy" }, store.New(), store.NewApplicationCredentials(), nil,
	)
	first, err := service.Authenticate(credentials)
	require.NoError(t, err)
	second, err := service.Authenticate(credentials)
	require.NoError(t, err)

	service.InvalidateTokens()
	fresh, err := service.Authenticate(credentials)
	require.NoError(t, err)

	assert.ErrorIs(t, service.Validate(first.ID), app.ErrInvalidToken)
	assert.ErrorIs(t, service.Validate(second.ID), app.ErrInvalidToken)
	assert.NoError(t, service.Validate(fresh.ID))
}

func TestConcurrentAuthenticationIssuesDistinctTokens(t *testing.T) {
	fixture, credentials := testIdentity()
	service := app.New(fixture, "password", time.Hour, time.Now,
		func() string { return "repeated entropy" }, store.New(), store.NewApplicationCredentials(), nil,
	)
	const attempts = 20
	var wait sync.WaitGroup
	var tokens [attempts]domain.Token
	var results [attempts]error

	for i := range attempts {
		wait.Go(func() { tokens[i], results[i] = service.Authenticate(credentials) })
	}
	wait.Wait()

	ids := make(map[string]bool, attempts)
	for i, token := range tokens {
		require.NoError(t, results[i])
		assert.False(t, ids[token.ID], "issued token ID more than once")
		assert.NoError(t, service.Validate(token.ID))
		ids[token.ID] = true
	}
}

func testIdentity() (domain.Fixture, domain.Credentials) {
	fixture := domain.Fixture{
		User: domain.User{
			ID:     "user-id",
			Name:   "user",
			Domain: domain.Domain{ID: "users-id", Name: "users"},
		},
		Project: domain.Project{
			ID:     "project-id",
			Name:   "project",
			Domain: domain.Domain{ID: "projects-id", Name: "projects"},
		},
		Role: domain.Role{ID: "role-id", Name: "member"},
	}
	return fixture, domain.Credentials{
		User:     domain.Selector{ID: fixture.User.ID},
		Project:  domain.Selector{ID: fixture.Project.ID},
		Password: "password",
	}
}
