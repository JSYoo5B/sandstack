package identity_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	app "github.com/JSYoo5B/sandstack/internal/app/identity"
	domain "github.com/JSYoo5B/sandstack/internal/domain/identity"
	store "github.com/JSYoo5B/sandstack/internal/store/memory/identity"
)

const secondPassword = "second-password"

func newMultiFixtureService(t *testing.T) (*app.Service, domain.Fixture, domain.Credentials, domain.Fixture) {
	t.Helper()
	primary, credentials := testIdentity()
	second := domain.Fixture{
		User:    domain.User{ID: "second-user-id", Name: "second-user", Domain: primary.User.Domain},
		Project: domain.Project{ID: "second-project-id", Name: "second-project", Domain: primary.Project.Domain},
		Role:    primary.Role,
	}
	service := app.New(primary, credentials.Password, time.Hour, time.Now,
		func() string { return "entropy" }, store.New(), store.NewApplicationCredentials(),
		bytes.NewReader(sequentialBytes(1024)),
	).WithFixture(second, secondPassword)
	return service, primary, credentials, second
}

func TestAuthenticateAdditionalFixture(t *testing.T) {
	service, primary, _, second := newMultiFixtureService(t)
	tests := map[string]domain.Credentials{
		"IDs": {
			User:     domain.Selector{ID: second.User.ID},
			Project:  domain.Selector{ID: second.Project.ID},
			Password: secondPassword,
		},
		"names and domain names": {
			User:     domain.Selector{Name: second.User.Name, DomainName: second.User.Domain.Name},
			Project:  domain.Selector{Name: second.Project.Name, DomainName: second.Project.Domain.Name},
			Password: secondPassword,
		},
	}
	for name, credentials := range tests {
		t.Run(name, func(t *testing.T) {
			token, err := service.Authenticate(credentials)

			require.NoError(t, err)
			assert.Equal(t, second, token.Fixture)
			projectID, err := service.ProjectForToken(token.ID)
			require.NoError(t, err)
			assert.Equal(t, second.Project.ID, projectID)
			userID, err := service.UserForToken(token.ID)
			require.NoError(t, err)
			assert.Equal(t, second.User.ID, userID)
			assert.NotEqual(t, primary.Project.ID, projectID)
		})
	}
}

// Keystone refuses to mint a project-scoped token without a role on the project.
// https://github.com/openstack/keystone/blob/ffeba71ada0492f95c0e370b65d324005130000b/keystone/models/token_model.py#L543-L550
func TestAuthenticateRefusesAnotherFixtureProject(t *testing.T) {
	service, primary, credentials, second := newMultiFixtureService(t)
	tests := map[string]struct {
		credentials domain.Credentials
		err         error
	}{
		"primary user on second project": {
			domain.Credentials{
				User:     credentials.User,
				Project:  domain.Selector{ID: second.Project.ID},
				Password: credentials.Password,
			},
			app.ErrNoProjectAccess,
		},
		"second user on primary project by name": {
			domain.Credentials{
				User:     domain.Selector{Name: second.User.Name, DomainID: second.User.Domain.ID},
				Project:  domain.Selector{Name: primary.Project.Name, DomainID: primary.Project.Domain.ID},
				Password: secondPassword,
			},
			app.ErrNoProjectAccess,
		},
		"second user with primary password": {
			domain.Credentials{
				User:     domain.Selector{ID: second.User.ID},
				Project:  domain.Selector{ID: second.Project.ID},
				Password: credentials.Password,
			},
			app.ErrInvalidCredentials,
		},
		"second user with wrong password on primary project": {
			domain.Credentials{
				User:     domain.Selector{ID: second.User.ID},
				Project:  credentials.Project,
				Password: "wrong",
			},
			app.ErrInvalidCredentials,
		},
		"second user on unknown project": {
			domain.Credentials{
				User:     domain.Selector{ID: second.User.ID},
				Project:  domain.Selector{ID: "unknown"},
				Password: secondPassword,
			},
			app.ErrInvalidCredentials,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			token, err := service.Authenticate(test.credentials)

			assert.ErrorIs(t, err, test.err)
			assert.Empty(t, token.ID)
		})
	}
}

func TestApplicationCredentialsBelongToTheirFixture(t *testing.T) {
	service, primary, credentials, second := newMultiFixtureService(t)
	primaryToken, err := service.Authenticate(credentials)
	require.NoError(t, err)
	secondToken, err := service.Authenticate(domain.Credentials{
		User:     domain.Selector{ID: second.User.ID},
		Project:  domain.Selector{ID: second.Project.ID},
		Password: secondPassword,
	})
	require.NoError(t, err)

	credential, secret, err := service.CreateApplicationCredential(
		secondToken.ID, second.User.ID, domain.ApplicationCredentialRequest{Name: "ci"},
	)

	require.NoError(t, err)
	assert.Equal(t, second.User.ID, credential.UserID)
	assert.Equal(t, second.Project.ID, credential.ProjectID)
	assert.Equal(t, []domain.Role{second.Role}, credential.Roles)

	_, _, err = service.CreateApplicationCredential(
		primaryToken.ID, second.User.ID, domain.ApplicationCredentialRequest{Name: "other"},
	)
	assert.ErrorIs(t, err, app.ErrForbidden)
	_, err = service.ListApplicationCredentials(primaryToken.ID, second.User.ID, "")
	assert.ErrorIs(t, err, app.ErrForbidden)
	_, err = service.GetApplicationCredential(primaryToken.ID, primary.User.ID, credential.ID)
	assert.ErrorIs(t, err, app.ErrForbidden)
	assert.ErrorIs(t, service.DeleteApplicationCredential(primaryToken.ID, primary.User.ID, credential.ID),
		app.ErrForbidden)
	primaryCredentials, err := service.ListApplicationCredentials(primaryToken.ID, primary.User.ID, "")
	require.NoError(t, err)
	assert.Empty(t, primaryCredentials)

	byID, err := service.AuthenticateApplicationCredential(
		domain.ApplicationCredentialAuth{ID: credential.ID, Secret: secret},
	)
	require.NoError(t, err)
	assert.Equal(t, second, byID.Fixture)
	projectID, err := service.ProjectForToken(byID.ID)
	require.NoError(t, err)
	assert.Equal(t, second.Project.ID, projectID)

	byName, err := service.AuthenticateApplicationCredential(domain.ApplicationCredentialAuth{
		Name: "ci", User: domain.Selector{Name: second.User.Name, DomainName: second.User.Domain.Name}, Secret: secret,
	})
	require.NoError(t, err)
	assert.Equal(t, second, byName.Fixture)

	_, err = service.AuthenticateApplicationCredential(domain.ApplicationCredentialAuth{
		Name: "ci", User: domain.Selector{ID: primary.User.ID}, Secret: secret,
	})
	assert.ErrorIs(t, err, app.ErrInvalidCredentials)
}

func TestTokenInvalidationCoversEveryFixture(t *testing.T) {
	service, _, credentials, second := newMultiFixtureService(t)
	secondCredentials := domain.Credentials{
		User:     domain.Selector{ID: second.User.ID},
		Project:  domain.Selector{ID: second.Project.ID},
		Password: secondPassword,
	}
	primaryToken, err := service.Authenticate(credentials)
	require.NoError(t, err)
	secondToken, err := service.Authenticate(secondCredentials)
	require.NoError(t, err)

	service.InvalidateToken(secondToken.ID)

	assert.NoError(t, service.Validate(primaryToken.ID))
	assert.ErrorIs(t, service.Validate(secondToken.ID), app.ErrInvalidToken)

	freshSecond, err := service.Authenticate(secondCredentials)
	require.NoError(t, err)

	service.InvalidateTokens()

	assert.ErrorIs(t, service.Validate(primaryToken.ID), app.ErrInvalidToken)
	assert.ErrorIs(t, service.Validate(freshSecond.ID), app.ErrInvalidToken)
}
