package server_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/servers"
	"github.com/gophercloud/gophercloud/v2/openstack/identity/v3/applicationcredentials"
	"github.com/gophercloud/gophercloud/v2/openstack/identity/v3/tokens"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func tokenUserID(t *testing.T, provider *gophercloud.ProviderClient) string {
	t.Helper()
	result, ok := provider.GetAuthResult().(tokens.CreateResult)
	require.True(t, ok)
	user, err := result.ExtractUser()
	require.NoError(t, err)
	require.NotEmpty(t, user.ID)
	return user.ID
}

// Keystone application credential management and authentication.
// https://docs.openstack.org/api-ref/identity/v3/#application-credentials
// https://github.com/openstack/keystone/blob/ffeba71ada0492f95c0e370b65d324005130000b/keystone/api/users.py
// https://github.com/openstack/keystone/blob/ffeba71ada0492f95c0e370b65d324005130000b/keystone/auth/core.py
func TestGophercloudAuthenticatesWithApplicationCredentials(t *testing.T) {
	instance := startServer(t, "")
	ctx := t.Context()
	provider := authenticate(t, instance)
	identity := identityClient(t, provider, instance.URL)
	userID := tokenUserID(t, provider)
	expiresAt := time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)

	created, err := applicationcredentials.Create(ctx, identity, userID, applicationcredentials.CreateOpts{
		Name:        "ci",
		Description: "CI deploy job",
		Roles:       []applicationcredentials.Role{{Name: "member"}},
		ExpiresAt:   &expiresAt,
	}).Extract()
	require.NoError(t, err)
	rotated, err := applicationcredentials.Create(ctx, identity, userID, applicationcredentials.CreateOpts{
		Name: "rotated", Secret: "provided-secret", Unrestricted: true,
	}).Extract()
	require.NoError(t, err)

	assert.Len(t, created.Secret, 86)
	assert.Len(t, created.ID, 32)
	assert.Equal(t, "ci", created.Name)
	assert.Equal(t, "CI deploy job", created.Description)
	assert.False(t, created.Unrestricted)
	assert.Equal(t, expiresAt, created.ExpiresAt)
	require.Len(t, created.Roles, 1)
	assert.Equal(t, "member", created.Roles[0].Name)
	assert.NotEmpty(t, created.ProjectID)
	assert.Equal(t, "provided-secret", rotated.Secret)
	assert.True(t, rotated.Unrestricted)
	assert.True(t, rotated.ExpiresAt.IsZero())

	_, duplicateErr := applicationcredentials.Create(ctx, identity, userID,
		applicationcredentials.CreateOpts{Name: "ci"}).Extract()
	_, otherUserErr := applicationcredentials.Create(ctx, identity, "other-user",
		applicationcredentials.CreateOpts{Name: "other"}).Extract()
	assert.True(t, gophercloud.ResponseCodeIs(duplicateErr, http.StatusConflict), "%v", duplicateErr)
	assert.True(t, gophercloud.ResponseCodeIs(otherUserErr, http.StatusForbidden), "%v", otherUserErr)

	list := func(client *gophercloud.ServiceClient, name string) []applicationcredentials.ApplicationCredential {
		t.Helper()
		pages, err := applicationcredentials.List(client, userID,
			applicationcredentials.ListOpts{Name: name}).AllPages(ctx)
		require.NoError(t, err)
		credentials, err := applicationcredentials.ExtractApplicationCredentials(pages)
		require.NoError(t, err)
		return credentials
	}
	read, err := applicationcredentials.Get(ctx, identity, userID, created.ID).Extract()
	require.NoError(t, err)
	expected := *created
	expected.Secret = ""
	assert.Equal(t, expected, *read)
	assert.Equal(t, []applicationcredentials.ApplicationCredential{expected}, list(identity, "ci"))
	assert.Len(t, list(identity, ""), 2)

	byID, err := openstack.AuthenticatedClient(ctx, gophercloud.AuthOptions{
		IdentityEndpoint:            instance.URL + identityPath,
		ApplicationCredentialID:     created.ID,
		ApplicationCredentialSecret: created.Secret,
	})
	require.NoError(t, err)
	byName := tokens.Create(ctx, identity, &tokens.AuthOptions{
		ApplicationCredentialName:   "ci",
		ApplicationCredentialSecret: created.Secret,
		Username:                    username,
		DomainName:                  "Default",
	})
	require.NoError(t, byName.Err)

	compute, err := openstack.NewComputeV2(byID, gophercloud.EndpointOpts{
		Region: "RegionOne", Availability: gophercloud.AvailabilityPublic,
	})
	require.NoError(t, err)
	pages, err := servers.List(compute, nil).AllPages(ctx)
	require.NoError(t, err)
	listed, err := servers.ExtractServers(pages)
	require.NoError(t, err)
	assert.Len(t, listed, 1)
	project, err := byName.ExtractProject()
	require.NoError(t, err)
	assert.Equal(t, created.ProjectID, project.ID)
	assert.Equal(t, projectName, project.Name)
	token, err := byName.ExtractToken()
	require.NoError(t, err)
	assert.Equal(t, expiresAt, token.ExpiresAt)
	catalog, err := byName.ExtractServiceCatalog()
	require.NoError(t, err)
	assert.Len(t, catalog.Entries, 5)
	assert.Equal(t, userID, tokenUserID(t, byID))

	restricted := identityClient(t, byID, instance.URL)
	_, restrictedCreateErr := applicationcredentials.Create(ctx, restricted, userID,
		applicationcredentials.CreateOpts{Name: "child"}).Extract()
	restrictedDeleteErr := applicationcredentials.Delete(ctx, restricted, userID, rotated.ID).ExtractErr()
	assert.True(t, gophercloud.ResponseCodeIs(restrictedCreateErr, http.StatusForbidden), "%v", restrictedCreateErr)
	assert.True(t, gophercloud.ResponseCodeIs(restrictedDeleteErr, http.StatusForbidden), "%v", restrictedDeleteErr)
	assert.Len(t, list(restricted, ""), 2)

	unrestrictedProvider, err := openstack.AuthenticatedClient(ctx, gophercloud.AuthOptions{
		IdentityEndpoint:            instance.URL + identityPath,
		ApplicationCredentialID:     rotated.ID,
		ApplicationCredentialSecret: rotated.Secret,
	})
	require.NoError(t, err)
	unrestricted := identityClient(t, unrestrictedProvider, instance.URL)
	child, err := applicationcredentials.Create(ctx, unrestricted, userID,
		applicationcredentials.CreateOpts{Name: "child"}).Extract()
	require.NoError(t, err)
	assert.Equal(t, created.ProjectID, child.ProjectID)
	assert.Equal(t, created.Roles, child.Roles)

	_, wrongSecretErr := openstack.AuthenticatedClient(ctx, gophercloud.AuthOptions{
		IdentityEndpoint:            instance.URL + identityPath,
		ApplicationCredentialID:     created.ID,
		ApplicationCredentialSecret: "wrong",
	})
	assert.True(t, gophercloud.ResponseCodeIs(wrongSecretErr, http.StatusUnauthorized), "%v", wrongSecretErr)

	require.NoError(t, applicationcredentials.Delete(ctx, identity, userID, created.ID).ExtractErr())

	_, err = applicationcredentials.Get(ctx, identity, userID, created.ID).Extract()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusNotFound), "%v", err)
	err = applicationcredentials.Delete(ctx, identity, userID, created.ID).ExtractErr()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusNotFound), "%v", err)
	_, err = servers.List(compute, nil).AllPages(ctx)
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusUnauthorized), "%v", err)
	_, err = openstack.AuthenticatedClient(ctx, gophercloud.AuthOptions{
		IdentityEndpoint:            instance.URL + identityPath,
		ApplicationCredentialID:     created.ID,
		ApplicationCredentialSecret: created.Secret,
	})
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusNotFound), "%v", err)
	byName = tokens.Create(ctx, identity, &tokens.AuthOptions{
		ApplicationCredentialName:   "ci",
		ApplicationCredentialSecret: created.Secret,
		UserID:                      userID,
	})
	assert.True(t, gophercloud.ResponseCodeIs(byName.Err, http.StatusUnauthorized), "%v", byName.Err)

	status, _, body := request(t, instance, http.MethodPost, "/_sandstack/reset", "",
		map[string]string{"X-SandStack-Admin-Token": adminToken})
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	identity = identityClient(t, authenticate(t, instance), instance.URL)

	assert.Empty(t, list(identity, ""))
	_, err = openstack.AuthenticatedClient(ctx, gophercloud.AuthOptions{
		IdentityEndpoint:            instance.URL + identityPath,
		ApplicationCredentialID:     rotated.ID,
		ApplicationCredentialSecret: rotated.Secret,
	})
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusNotFound), "%v", err)
}

func TestApplicationCredentialTokenInvalidationKeepsCredentials(t *testing.T) {
	instance := startServer(t, "")
	ctx := t.Context()
	provider := authenticate(t, instance)
	userID := tokenUserID(t, provider)
	created, err := applicationcredentials.Create(ctx, identityClient(t, provider, instance.URL), userID,
		applicationcredentials.CreateOpts{Name: "ci"}).Extract()
	require.NoError(t, err)
	options := gophercloud.AuthOptions{
		IdentityEndpoint:            instance.URL + identityPath,
		ApplicationCredentialID:     created.ID,
		ApplicationCredentialSecret: created.Secret,
		AllowReauth:                 true,
	}
	credentialProvider, err := openstack.AuthenticatedClient(ctx, options)
	require.NoError(t, err)
	client := identityClient(t, credentialProvider, instance.URL)
	original := credentialProvider.Token()

	status, _, body := request(t, instance, http.MethodPost, invalidatePath, "",
		map[string]string{"X-SandStack-Admin-Token": adminToken})
	require.Equal(t, http.StatusNoContent, status, "%s", body)

	checkClientCatalog(t, client, instance.URL)
	assert.NotEqual(t, original, credentialProvider.Token())
}

func TestApplicationCredentialWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	userID := tokenUserID(t, provider)
	collection := identityPath + "/users/" + userID + "/application_credentials"
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	decode := func(body []byte) map[string]json.RawMessage {
		t.Helper()
		var envelope struct {
			Credential map[string]json.RawMessage `json:"application_credential"`
		}
		require.NoError(t, json.Unmarshal(body, &envelope))
		return envelope.Credential
	}
	keys := func(fields map[string]json.RawMessage) []string {
		names := make([]string, 0, len(fields))
		for name := range fields {
			names = append(names, name)
		}
		return names
	}

	// Create filters to public parameters and returns the secret once.
	// https://github.com/openstack/keystone/blob/ffeba71ada0492f95c0e370b65d324005130000b/api-ref/source/v3/samples/admin/application-credential-create-response.json
	status, headers, body := request(t, instance, http.MethodPost, collection,
		`{"application_credential":{"name":"monitoring","expires_at":"2099-02-27T18:30:59Z",`+
			`"roles":[{"name":"member"}],"unrestricted":false}}`, auth)

	require.Equal(t, http.StatusCreated, status, "%s", body)
	requireJSON(t, headers)
	created := decode(body)
	assert.ElementsMatch(t, []string{
		"id", "name", "description", "expires_at", "project_id", "roles", "unrestricted", "links", "secret",
	}, keys(created))
	var id, secret string
	require.NoError(t, json.Unmarshal(created["id"], &id))
	require.NoError(t, json.Unmarshal(created["secret"], &secret))
	assert.Regexp(t, `^[0-9a-f]{32}$`, id)
	assert.Regexp(t, `^[A-Za-z0-9_-]{86}$`, secret)
	assert.JSONEq(t, `null`, string(created["description"]))
	assert.JSONEq(t, `"2099-02-27T18:30:59.000000"`, string(created["expires_at"]))
	assert.JSONEq(t, `false`, string(created["unrestricted"]))
	var roles []map[string]any
	require.NoError(t, json.Unmarshal(created["roles"], &roles))
	require.Len(t, roles, 1)
	assert.Equal(t, "member", roles[0]["name"])
	assert.Contains(t, roles[0], "domain_id")
	assert.Nil(t, roles[0]["domain_id"])
	assert.JSONEq(t, fmt.Sprintf(`{"self":%q}`, instance.URL+collection+"/"+id), string(created["links"]))

	status, _, body = request(t, instance, http.MethodGet, collection+"/"+id, "", auth)

	require.Equal(t, http.StatusOK, status, "%s", body)
	read := decode(body)
	assert.NotContains(t, read, "secret")
	assert.JSONEq(t, fmt.Sprintf("%q", userID), string(read["user_id"]))
	assert.JSONEq(t, `null`, string(read["system"]))

	status, _, body = request(t, instance, http.MethodHead, collection+"/"+id, "", auth)
	assert.Equal(t, http.StatusOK, status)
	assert.Empty(t, body)

	status, _, body = request(t, instance, http.MethodGet, collection+"?name=missing", "", auth)

	require.Equal(t, http.StatusOK, status, "%s", body)
	assert.JSONEq(t, fmt.Sprintf(`{"application_credentials":[],"links":{"self":%q,"previous":null,"next":null}}`,
		instance.URL+collection), string(body))

	// Request bodies follow the Keystone create schema and the unsupported
	// fields documented in the guide are rejected.
	// https://github.com/openstack/keystone/blob/ffeba71ada0492f95c0e370b65d324005130000b/keystone/application_credential/schema.py
	for name, test := range map[string]struct {
		body   string
		status int
	}{
		"missing name":       {`{"application_credential":{}}`, http.StatusBadRequest},
		"blank name":         {`{"application_credential":{"name":"  "}}`, http.StatusBadRequest},
		"unknown field":      {`{"application_credential":{"name":"x","extra":1}}`, http.StatusBadRequest},
		"role with id+name":  {`{"application_credential":{"name":"x","roles":[{"id":"a","name":"b"}]}}`, http.StatusBadRequest},
		"invalid expiration": {`{"application_credential":{"name":"x","expires_at":"next tuesday"}}`, http.StatusBadRequest},
		"past expiration":    {`{"application_credential":{"name":"x","expires_at":"2000-01-01T00:00:00"}}`, http.StatusBadRequest},
		"unassigned role":    {`{"application_credential":{"name":"x","roles":[{"id":"unassigned"}]}}`, http.StatusBadRequest},
		"access rules": {
			`{"application_credential":{"name":"x","access_rules":[{"path":"/v2.1/servers","method":"GET","service":"compute"}]}}`,
			http.StatusBadRequest,
		},
		"unknown role name": {`{"application_credential":{"name":"x","roles":[{"name":"admin"}]}}`, http.StatusNotFound},
		"duplicate name":    {`{"application_credential":{"name":"monitoring"}}`, http.StatusConflict},
	} {
		t.Run(name, func(t *testing.T) {
			status, _, body := request(t, instance, http.MethodPost, collection, test.body, auth)

			require.Equal(t, test.status, status, "%s", body)
			assertWireError(t, body, test.status)
		})
	}

	status, _, body = request(t, instance, http.MethodGet, collection, "", nil)
	require.Equal(t, http.StatusUnauthorized, status, "%s", body)
	assertWireError(t, body, http.StatusUnauthorized)
	status, _, body = request(t, instance, http.MethodGet, identityPath+"/users/other/application_credentials", "", auth)
	require.Equal(t, http.StatusForbidden, status, "%s", body)
	assertWireError(t, body, http.StatusForbidden)

	// Application credential authentication takes its scope from the
	// credential and reports the credential in the token.
	// https://github.com/openstack/keystone/blob/ffeba71ada0492f95c0e370b65d324005130000b/api-ref/source/v3/samples/admin/auth-application-credential-response.json
	idRequest := fmt.Sprintf(`{"auth":{"identity":{"methods":["application_credential"],`+
		`"application_credential":{"id":%q,"secret":%q}}}}`, id, secret)
	status, headers, body = request(t, instance, http.MethodPost, tokensPath, idRequest, nil)

	require.Equal(t, http.StatusCreated, status, "%s", body)
	credentialToken := headers.Get("X-Subject-Token")
	require.NotEmpty(t, credentialToken)
	var issued struct {
		Token struct {
			Methods               []string         `json:"methods"`
			Project               namedResource    `json:"project"`
			Roles                 []namedResource  `json:"roles"`
			Catalog               []catalogService `json:"catalog"`
			ApplicationCredential map[string]any   `json:"application_credential"`
		} `json:"token"`
	}
	require.NoError(t, json.Unmarshal(body, &issued))
	assert.Equal(t, []string{"application_credential"}, issued.Token.Methods)
	assertResource(t, issued.Token.Project, projectName)
	require.Len(t, issued.Token.Roles, 1)
	assert.Equal(t, "member", issued.Token.Roles[0].Name)
	assertCatalog(t, issued.Token.Catalog, instance.URL, issued.Token.Project.ID)
	assert.Equal(t, map[string]any{"id": id, "name": "monitoring", "restricted": true},
		issued.Token.ApplicationCredential)

	nameRequest := fmt.Sprintf(`{"auth":{"identity":{"methods":["application_credential"],`+
		`"application_credential":{"name":"monitoring","secret":%q,"user":{"id":%q}}}}}`, secret, userID)
	status, _, body = request(t, instance, http.MethodPost, tokensPath, nameRequest, nil)
	require.Equal(t, http.StatusCreated, status, "%s", body)

	scopedRequest := fmt.Sprintf(`{"auth":{"identity":{"methods":["application_credential"],`+
		`"application_credential":{"id":%q,"secret":%q}},"scope":{"project":{"id":"x"}}}}`, id, secret)
	for name, test := range map[string]struct {
		body   string
		status int
	}{
		"wrong secret":       {strings.Replace(idRequest, secret, "wrong", 1), http.StatusUnauthorized},
		"scope requested":    {scopedRequest, http.StatusUnauthorized},
		"unknown name":       {strings.Replace(nameRequest, "monitoring", "missing", 1), http.StatusUnauthorized},
		"name without user":  {`{"auth":{"identity":{"methods":["application_credential"],"application_credential":{"name":"monitoring","secret":"s"}}}}`, http.StatusBadRequest},
		"unknown credential": {strings.Replace(idRequest, id, "missing", 1), http.StatusNotFound},
	} {
		t.Run(name, func(t *testing.T) {
			status, headers, body := request(t, instance, http.MethodPost, tokensPath, test.body, nil)

			require.Equal(t, test.status, status, "%s", body)
			assert.Empty(t, headers.Get("X-Subject-Token"))
			assertWireError(t, body, test.status)
		})
	}

	// Deleting the credential revokes the tokens issued from it.
	// https://github.com/openstack/keystone/blob/ffeba71ada0492f95c0e370b65d324005130000b/keystone/models/token_model.py
	credentialAuth := map[string]string{"X-Auth-Token": credentialToken}
	status, _, body = request(t, instance, http.MethodDelete, collection+"/"+id, "", credentialAuth)
	require.Equal(t, http.StatusForbidden, status, "%s", body)
	assertWireError(t, body, http.StatusForbidden)

	status, _, body = request(t, instance, http.MethodDelete, collection+"/"+id, "", auth)

	require.Equal(t, http.StatusNoContent, status, "%s", body)
	assert.Empty(t, body)
	status, _, body = request(t, instance, http.MethodGet, catalogPath, "", credentialAuth)
	require.Equal(t, http.StatusUnauthorized, status, "%s", body)
	status, _, body = request(t, instance, http.MethodGet, collection+"/"+id, "", auth)
	require.Equal(t, http.StatusNotFound, status, "%s", body)
	assertWireError(t, body, http.StatusNotFound)
	status, _, body = request(t, instance, http.MethodPost, tokensPath, idRequest, nil)
	require.Equal(t, http.StatusNotFound, status, "%s", body)
	assertWireError(t, body, http.StatusNotFound)
}
