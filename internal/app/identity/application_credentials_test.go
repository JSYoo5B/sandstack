package identity_test

import (
	"bytes"
	"encoding/base64"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	app "github.com/JSYoo5B/sandstack/internal/app/identity"
	domain "github.com/JSYoo5B/sandstack/internal/domain/identity"
	store "github.com/JSYoo5B/sandstack/internal/store/memory/identity"
)

type credentialFixture struct {
	service  *app.Service
	fixture  domain.Fixture
	password domain.Token
	now      *time.Time
}

func newCredentialFixture(t *testing.T, entropy []byte) credentialFixture {
	t.Helper()
	fixture, credentials := testIdentity()
	now := time.Date(2026, time.November, 28, 0, 0, 0, 0, time.UTC)
	service := app.New(fixture, "password", time.Hour,
		func() time.Time { return now }, func() string { return "entropy" },
		store.New(), store.NewApplicationCredentials(), bytes.NewReader(entropy),
	)
	token, err := service.Authenticate(credentials)
	require.NoError(t, err)
	return credentialFixture{service: service, fixture: fixture, password: token, now: &now}
}

func sequentialBytes(n int) []byte {
	data := make([]byte, n)
	for i := range data {
		data[i] = byte(i)
	}
	return data
}

// Keystone application credential create, list, get, and delete handlers.
// https://github.com/openstack/keystone/blob/ffeba71ada0492f95c0e370b65d324005130000b/keystone/api/users.py
func TestCreateApplicationCredentialGeneratesSecretAndInheritsTokenScope(t *testing.T) {
	entropy := sequentialBytes(96)
	f := newCredentialFixture(t, entropy)
	expiresAt := f.now.Add(24 * time.Hour)

	credential, secret, err := f.service.CreateApplicationCredential(f.password.ID, f.fixture.User.ID,
		domain.ApplicationCredentialRequest{Name: "ci", ExpiresAt: &expiresAt})

	require.NoError(t, err)
	assert.Equal(t, base64.RawURLEncoding.EncodeToString(entropy[:64]), secret)
	assert.Len(t, secret, 86)
	assert.Equal(t, "404142434445464788494a4b4c4d4e4f", credential.ID)
	assert.Equal(t, entropy[80:96], credential.SecretSalt)
	assert.NotContains(t, string(credential.SecretHash), secret)
	assert.Len(t, credential.SecretHash, 64)
	assert.Equal(t, f.fixture.User.ID, credential.UserID)
	assert.Equal(t, f.fixture.Project.ID, credential.ProjectID)
	assert.Equal(t, []domain.Role{f.fixture.Role}, credential.Roles)
	assert.Equal(t, &expiresAt, credential.ExpiresAt)
	assert.True(t, credential.Restricted())
}

func TestCreateApplicationCredentialRejectsInvalidRequests(t *testing.T) {
	f := newCredentialFixture(t, sequentialBytes(256))
	past := f.now.Add(-time.Second)
	_, _, err := f.service.CreateApplicationCredential(f.password.ID, f.fixture.User.ID,
		domain.ApplicationCredentialRequest{Name: "taken", Secret: "secret"})
	require.NoError(t, err)

	tests := map[string]struct {
		userID  string
		request domain.ApplicationCredentialRequest
		err     error
	}{
		"other user": {"other", domain.ApplicationCredentialRequest{Name: "other"}, app.ErrForbidden},
		"duplicate name": {
			f.fixture.User.ID, domain.ApplicationCredentialRequest{Name: "taken"}, app.ErrApplicationCredentialExists,
		},
		"unassigned role ID": {
			f.fixture.User.ID,
			domain.ApplicationCredentialRequest{Name: "role", Roles: []domain.Selector{{ID: "unassigned"}}},
			app.ErrInvalidApplicationCredential,
		},
		"unknown role name": {
			f.fixture.User.ID,
			domain.ApplicationCredentialRequest{Name: "role", Roles: []domain.Selector{{Name: "admin"}}},
			app.ErrRoleNotFound,
		},
		"already expired": {
			f.fixture.User.ID,
			domain.ApplicationCredentialRequest{Name: "expired", ExpiresAt: &past},
			app.ErrInvalidApplicationCredential,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			_, _, err := f.service.CreateApplicationCredential(f.password.ID, test.userID, test.request)

			assert.ErrorIs(t, err, test.err)
		})
	}

	listed, err := f.service.ListApplicationCredentials(f.password.ID, f.fixture.User.ID, "")
	require.NoError(t, err)
	assert.Len(t, listed, 1)
}

func TestCreateApplicationCredentialAcceptsAssignedRoleSelectors(t *testing.T) {
	f := newCredentialFixture(t, sequentialBytes(256))

	credential, secret, err := f.service.CreateApplicationCredential(f.password.ID, f.fixture.User.ID,
		domain.ApplicationCredentialRequest{
			Name:   "roles",
			Secret: "provided",
			Roles:  []domain.Selector{{ID: f.fixture.Role.ID}, {Name: f.fixture.Role.Name}},
		})

	require.NoError(t, err)
	assert.Equal(t, "provided", secret)
	assert.Equal(t, []domain.Role{f.fixture.Role}, credential.Roles)
}

func TestCreateApplicationCredentialFailsWithoutEntropy(t *testing.T) {
	f := newCredentialFixture(t, sequentialBytes(10))

	_, _, err := f.service.CreateApplicationCredential(f.password.ID, f.fixture.User.ID,
		domain.ApplicationCredentialRequest{Name: "ci"})

	assert.ErrorIs(t, err, app.ErrApplicationCredentialGeneration)
}

func TestApplicationCredentialOwnership(t *testing.T) {
	f := newCredentialFixture(t, sequentialBytes(256))
	created, _, err := f.service.CreateApplicationCredential(f.password.ID, f.fixture.User.ID,
		domain.ApplicationCredentialRequest{Name: "ci", Secret: "secret"})
	require.NoError(t, err)
	_, _, err = f.service.CreateApplicationCredential(f.password.ID, f.fixture.User.ID,
		domain.ApplicationCredentialRequest{Name: "other", Secret: "secret"})
	require.NoError(t, err)

	named, err := f.service.ListApplicationCredentials(f.password.ID, f.fixture.User.ID, "ci")
	require.NoError(t, err)
	assert.Equal(t, []domain.ApplicationCredential{created}, named)
	_, err = f.service.ListApplicationCredentials(f.password.ID, "other", "")
	assert.ErrorIs(t, err, app.ErrForbidden)

	read, err := f.service.GetApplicationCredential(f.password.ID, "other", created.ID)
	require.NoError(t, err)
	assert.Equal(t, created, read)
	_, err = f.service.GetApplicationCredential(f.password.ID, "other", "missing")
	assert.ErrorIs(t, err, app.ErrForbidden)
	_, err = f.service.GetApplicationCredential(f.password.ID, f.fixture.User.ID, "missing")
	assert.ErrorIs(t, err, app.ErrApplicationCredentialNotFound)
	_, err = f.service.GetApplicationCredential("invalid", f.fixture.User.ID, created.ID)
	assert.ErrorIs(t, err, app.ErrInvalidToken)

	require.NoError(t, f.service.DeleteApplicationCredential(f.password.ID, f.fixture.User.ID, created.ID))

	err = f.service.DeleteApplicationCredential(f.password.ID, f.fixture.User.ID, created.ID)
	assert.ErrorIs(t, err, app.ErrApplicationCredentialNotFound)
	remaining, err := f.service.ListApplicationCredentials(f.password.ID, f.fixture.User.ID, "")
	require.NoError(t, err)
	require.Len(t, remaining, 1)
	assert.Equal(t, "other", remaining[0].Name)
}

// Keystone application credential authentication and token issuance.
// https://github.com/openstack/keystone/blob/ffeba71ada0492f95c0e370b65d324005130000b/keystone/application_credential/backends/sql.py
// https://github.com/openstack/keystone/blob/ffeba71ada0492f95c0e370b65d324005130000b/keystone/token/provider.py
func TestAuthenticateApplicationCredential(t *testing.T) {
	f := newCredentialFixture(t, sequentialBytes(256))
	expiresAt := f.now.Add(30 * time.Minute)
	credential, secret, err := f.service.CreateApplicationCredential(f.password.ID, f.fixture.User.ID,
		domain.ApplicationCredentialRequest{Name: "ci", ExpiresAt: &expiresAt})
	require.NoError(t, err)

	byID, err := f.service.AuthenticateApplicationCredential(
		domain.ApplicationCredentialAuth{ID: credential.ID, Secret: secret})
	require.NoError(t, err)
	byName, err := f.service.AuthenticateApplicationCredential(domain.ApplicationCredentialAuth{
		Name:   "ci",
		User:   domain.Selector{Name: f.fixture.User.Name, DomainName: f.fixture.User.Domain.Name},
		Secret: secret,
	})
	require.NoError(t, err)

	require.NotNil(t, byID.ApplicationCredential)
	assert.Equal(t, credential.ID, byID.ApplicationCredential.ID)
	assert.Equal(t, expiresAt, byID.ExpiresAt)
	assert.Equal(t, credential.ID, byName.ApplicationCredential.ID)
	assert.NoError(t, f.service.Validate(byName.ID))

	tests := map[string]struct {
		auth domain.ApplicationCredentialAuth
		err  error
	}{
		"wrong secret":   {domain.ApplicationCredentialAuth{ID: credential.ID, Secret: "wrong"}, app.ErrInvalidCredentials},
		"missing secret": {domain.ApplicationCredentialAuth{ID: credential.ID}, app.ErrInvalidCredentials},
		"unknown ID":     {domain.ApplicationCredentialAuth{ID: "missing", Secret: secret}, app.ErrApplicationCredentialNotFound},
		"unknown name": {
			domain.ApplicationCredentialAuth{Name: "missing", User: domain.Selector{ID: f.fixture.User.ID}, Secret: secret},
			app.ErrInvalidCredentials,
		},
		"other user": {
			domain.ApplicationCredentialAuth{Name: "ci", User: domain.Selector{ID: "other"}, Secret: secret},
			app.ErrInvalidCredentials,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := f.service.AuthenticateApplicationCredential(test.auth)

			assert.ErrorIs(t, err, test.err)
		})
	}

	*f.now = expiresAt.Add(-time.Nanosecond)
	assert.NoError(t, f.service.Validate(byID.ID))
	*f.now = expiresAt
	assert.ErrorIs(t, f.service.Validate(byID.ID), app.ErrInvalidToken)
	_, err = f.service.AuthenticateApplicationCredential(
		domain.ApplicationCredentialAuth{ID: credential.ID, Secret: secret})
	assert.ErrorIs(t, err, app.ErrInvalidCredentials)
}

func TestRestrictedApplicationCredentialTokenCannotManageCredentials(t *testing.T) {
	f := newCredentialFixture(t, sequentialBytes(512))
	unrestricted := true
	restricted, restrictedSecret, err := f.service.CreateApplicationCredential(f.password.ID, f.fixture.User.ID,
		domain.ApplicationCredentialRequest{Name: "restricted"})
	require.NoError(t, err)
	parent, parentSecret, err := f.service.CreateApplicationCredential(f.password.ID, f.fixture.User.ID,
		domain.ApplicationCredentialRequest{Name: "unrestricted", Unrestricted: &unrestricted})
	require.NoError(t, err)
	restrictedToken, err := f.service.AuthenticateApplicationCredential(
		domain.ApplicationCredentialAuth{ID: restricted.ID, Secret: restrictedSecret})
	require.NoError(t, err)
	parentToken, err := f.service.AuthenticateApplicationCredential(
		domain.ApplicationCredentialAuth{ID: parent.ID, Secret: parentSecret})
	require.NoError(t, err)

	_, _, createErr := f.service.CreateApplicationCredential(restrictedToken.ID, f.fixture.User.ID,
		domain.ApplicationCredentialRequest{Name: "child"})
	deleteErr := f.service.DeleteApplicationCredential(restrictedToken.ID, f.fixture.User.ID, parent.ID)
	missingErr := f.service.DeleteApplicationCredential(restrictedToken.ID, f.fixture.User.ID, "missing")
	listed, listErr := f.service.ListApplicationCredentials(restrictedToken.ID, f.fixture.User.ID, "")
	_, getErr := f.service.GetApplicationCredential(restrictedToken.ID, f.fixture.User.ID, parent.ID)

	assert.ErrorIs(t, createErr, app.ErrForbidden)
	assert.ErrorIs(t, deleteErr, app.ErrForbidden)
	assert.ErrorIs(t, missingErr, app.ErrForbidden)
	assert.NoError(t, listErr)
	assert.Len(t, listed, 2)
	assert.NoError(t, getErr)

	child, _, err := f.service.CreateApplicationCredential(parentToken.ID, f.fixture.User.ID,
		domain.ApplicationCredentialRequest{Name: "child"})

	require.NoError(t, err)
	assert.Equal(t, f.fixture.Project.ID, child.ProjectID)
	assert.Equal(t, []domain.Role{f.fixture.Role}, child.Roles)
	assert.NoError(t, f.service.DeleteApplicationCredential(parentToken.ID, f.fixture.User.ID, child.ID))
}

func TestDeletingApplicationCredentialInvalidatesItsTokens(t *testing.T) {
	f := newCredentialFixture(t, sequentialBytes(256))
	credential, secret, err := f.service.CreateApplicationCredential(f.password.ID, f.fixture.User.ID,
		domain.ApplicationCredentialRequest{Name: "ci"})
	require.NoError(t, err)
	token, err := f.service.AuthenticateApplicationCredential(
		domain.ApplicationCredentialAuth{ID: credential.ID, Secret: secret})
	require.NoError(t, err)
	require.NoError(t, f.service.Validate(token.ID))

	require.NoError(t, f.service.DeleteApplicationCredential(f.password.ID, f.fixture.User.ID, credential.ID))

	assert.ErrorIs(t, f.service.Validate(token.ID), app.ErrInvalidToken)
	_, err = f.service.ProjectForToken(token.ID)
	assert.ErrorIs(t, err, app.ErrInvalidToken)
	assert.NoError(t, f.service.Validate(f.password.ID))
}

func TestResetRemovesApplicationCredentialsButTokenInvalidationKeepsThem(t *testing.T) {
	f := newCredentialFixture(t, sequentialBytes(256))
	_, credentials := testIdentity()
	credential, secret, err := f.service.CreateApplicationCredential(f.password.ID, f.fixture.User.ID,
		domain.ApplicationCredentialRequest{Name: "ci"})
	require.NoError(t, err)
	auth := domain.ApplicationCredentialAuth{ID: credential.ID, Secret: secret}

	f.service.InvalidateTokens()

	_, err = f.service.AuthenticateApplicationCredential(auth)
	require.NoError(t, err)

	f.service.Reset()

	_, err = f.service.AuthenticateApplicationCredential(auth)
	assert.ErrorIs(t, err, app.ErrApplicationCredentialNotFound)
	token, err := f.service.Authenticate(credentials)
	require.NoError(t, err)
	listed, err := f.service.ListApplicationCredentials(token.ID, f.fixture.User.ID, "")
	require.NoError(t, err)
	assert.Empty(t, listed)
}
