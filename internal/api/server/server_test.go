package server_test

import (
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/JSYoo5B/sandstack/internal/api/server"
	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/identity/v3/catalog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	username       = "identity-reader"
	password       = "identity-test-password"
	projectName    = "identity-test-project"
	adminToken     = "identity-test-control-token"
	identityPath   = "/identity/v3"
	tokensPath     = identityPath + "/auth/tokens"
	catalogPath    = identityPath + "/auth/catalog"
	invalidatePath = "/_sandstack/identity/invalidate-tokens"
)

const scopedPasswordRequest = `{
  "auth": {
    "identity": {
      "methods": ["password"],
      "password": {"user": {
        "name": "` + username + `",
        "domain": {"name": "Default"},
        "password": "` + password + `"
      }}
    },
    "scope": {"project": {"name": "` + projectName + `", "domain": {"name": "Default"}}}
  }
}`

func TestScopedPasswordTokenWire(t *testing.T) {
	const publicOrigin = "https://identity.example.test"
	instance := startServer(t, publicOrigin)

	// OpenStack Identity v3 password authentication with scoped authorization.
	// https://docs.openstack.org/api-ref/identity/v3/?expanded=password-authentication-with-scoped-authorization-detail
	status, headers, body := request(t, instance, http.MethodPost, tokensPath, scopedPasswordRequest, nil)

	require.Equal(t, http.StatusCreated, status, "%s", body)
	requireJSON(t, headers)
	tokenID := headers.Get("X-Subject-Token")
	require.NotEmpty(t, tokenID)
	var result struct {
		Token struct {
			Methods   []string         `json:"methods"`
			IssuedAt  time.Time        `json:"issued_at"`
			ExpiresAt time.Time        `json:"expires_at"`
			AuditIDs  []string         `json:"audit_ids"`
			User      namedResource    `json:"user"`
			Project   namedResource    `json:"project"`
			Roles     []namedResource  `json:"roles"`
			Catalog   []catalogService `json:"catalog"`
		} `json:"token"`
	}
	require.NoError(t, json.Unmarshal(body, &result))
	issued := result.Token
	assert.Equal(t, []string{"password"}, issued.Methods)
	assert.False(t, issued.IssuedAt.IsZero())
	assert.True(t, issued.ExpiresAt.After(issued.IssuedAt))
	require.Len(t, issued.AuditIDs, 1)
	assert.NotEmpty(t, issued.AuditIDs[0])
	require.NotEmpty(t, issued.User.ID)
	require.NotEmpty(t, issued.Project.ID)
	require.NotEmpty(t, issued.Roles)
	assert.NotEmpty(t, issued.Roles[0].ID)
	assert.NotEmpty(t, issued.Roles[0].Name)

	// These values are from the SandStack fixture and endpoint configuration.
	assert.WithinDuration(t, issued.IssuedAt.Add(time.Hour), issued.ExpiresAt, time.Second)
	assertResource(t, issued.User, username)
	assertResource(t, issued.Project, projectName)
	assertCatalog(t, issued.Catalog, publicOrigin, issued.Project.ID)

	// The Identity v3 project ID and user ID selectors need no domain selector.
	idBasedRequest := fmt.Sprintf(`{"auth":{
  "identity":{"methods":["password"],"password":{"user":{"id":%q,"password":%q}}},
  "scope":{"project":{"id":%q}}
}}`, issued.User.ID, password, issued.Project.ID)
	status, headers, body = request(t, instance, http.MethodPost, tokensPath, idBasedRequest, nil)

	require.Equal(t, http.StatusCreated, status, "%s", body)
	require.NotEmpty(t, headers.Get("X-Subject-Token"))
	var byID struct {
		Token struct {
			User    namedResource `json:"user"`
			Project namedResource `json:"project"`
		} `json:"token"`
	}
	require.NoError(t, json.Unmarshal(body, &byID))
	assert.Equal(t, issued.User, byID.Token.User)
	assert.Equal(t, issued.Project, byID.Token.Project)

	badCredentials := strings.Replace(scopedPasswordRequest, password, "wrong-password", 1)
	status, headers, body = request(t, instance, http.MethodPost, tokensPath, badCredentials, nil)

	require.Equal(t, http.StatusUnauthorized, status, "%s", body)
	assert.Empty(t, headers.Get("X-Subject-Token"))
	assertWireError(t, body, http.StatusUnauthorized)

	// The catalog endpoint returns the same entries as the scoped token.
	// https://docs.openstack.org/api-ref/identity/v3/?expanded=get-service-catalog-detail
	auth := map[string]string{"X-Auth-Token": tokenID}
	status, headers, body = request(t, instance, http.MethodGet, catalogPath, "", auth)

	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	getContentType := headers.Get("Content-Type")
	var listing struct {
		Catalog []catalogService `json:"catalog"`
		Links   struct {
			Self string `json:"self"`
		} `json:"links"`
	}
	require.NoError(t, json.Unmarshal(body, &listing))
	assert.Equal(t, issued.Catalog, listing.Catalog)
	assert.Equal(t, publicOrigin+catalogPath, listing.Links.Self)

	status, headers, body = request(t, instance, http.MethodHead, catalogPath, "", auth)

	assert.Equal(t, http.StatusOK, status)
	assert.Equal(t, getContentType, headers.Get("Content-Type"))
	assert.Empty(t, body)

	status, _, body = request(t, instance, http.MethodGet, catalogPath, "", nil)

	require.Equal(t, http.StatusUnauthorized, status, "%s", body)
	assertWireError(t, body, http.StatusUnauthorized)
}

func TestGophercloudRecoversFromInvalidatedTokens(t *testing.T) {
	instance := startServer(t, "")
	first := authenticate(t, instance)
	second := authenticate(t, instance)
	assert.NotEqual(t, first.Token(), second.Token())

	firstClient := identityClient(t, first, instance.URL)
	secondClient := identityClient(t, second, instance.URL)
	checkClientCatalog(t, firstClient, instance.URL)
	checkClientCatalog(t, secondClient, instance.URL)

	for _, credential := range []string{"", first.Token(), "wrong-admin-token"} {
		status, _, _ := request(t, instance, http.MethodPost, invalidatePath, "", map[string]string{
			"X-SandStack-Admin-Token": credential,
		})

		assert.Equal(t, http.StatusUnauthorized, status)
		checkClientCatalog(t, firstClient, instance.URL)
		checkClientCatalog(t, secondClient, instance.URL)
	}

	status, _, body := request(t, instance, http.MethodPost, invalidatePath, "", map[string]string{
		"X-SandStack-Admin-Token": adminToken,
	})

	require.Equal(t, http.StatusNoContent, status, "%s", body)
	assertClientUnauthorized(t, firstClient)
	assertClientUnauthorized(t, secondClient)

	fresh := authenticate(t, instance)
	assert.NotEqual(t, first.Token(), fresh.Token())
	assert.NotEqual(t, second.Token(), fresh.Token())
	checkClientCatalog(t, identityClient(t, fresh, instance.URL), instance.URL)
	assertClientUnauthorized(t, firstClient)
}

func TestGophercloudReauthenticatesAfterTargetedInvalidation(t *testing.T) {
	const targetPath = "/_sandstack/identity/invalidate-token"
	instance := startServer(t, "")
	options := authOptions(instance)
	options.AllowReauth = true
	target, err := openstack.AuthenticatedClient(t.Context(), options)
	require.NoError(t, err)
	oldToken := target.Token()
	require.NotEmpty(t, oldToken)
	untouched := authenticate(t, instance)
	untouchedToken := untouched.Token()
	require.NotEqual(t, oldToken, untouchedToken)
	targetClient := identityClient(t, target, instance.URL)
	untouchedClient := identityClient(t, untouched, instance.URL)
	checkClientCatalog(t, targetClient, instance.URL)
	checkClientCatalog(t, untouchedClient, instance.URL)

	status, _, body := request(t, instance, http.MethodPost, targetPath, "", map[string]string{
		"X-SandStack-Admin-Token": "wrong-admin-token",
		"X-Subject-Token":         oldToken,
	})
	require.Equal(t, http.StatusUnauthorized, status, "%s", body)

	status, _, body = request(t, instance, http.MethodPost, targetPath, "", map[string]string{
		"X-SandStack-Admin-Token": adminToken,
	})
	require.Equal(t, http.StatusBadRequest, status, "%s", body)
	checkClientCatalog(t, targetClient, instance.URL)
	assert.Equal(t, oldToken, target.Token())
	checkClientCatalog(t, untouchedClient, instance.URL)

	for _, subject := range []string{oldToken, oldToken, "never-issued-token"} {
		status, _, body = request(t, instance, http.MethodPost, targetPath, "", map[string]string{
			"X-SandStack-Admin-Token": adminToken,
			"X-Subject-Token":         subject,
		})
		require.Equal(t, http.StatusNoContent, status, "%s", body)
	}
	status, _, body = request(t, instance, http.MethodGet, catalogPath, "", map[string]string{
		"X-Auth-Token": oldToken,
	})
	require.Equal(t, http.StatusUnauthorized, status, "%s", body)
	assertWireError(t, body, http.StatusUnauthorized)
	checkClientCatalog(t, untouchedClient, instance.URL)
	assert.Equal(t, untouchedToken, untouched.Token())

	checkClientCatalog(t, targetClient, instance.URL)
	assert.NotEmpty(t, target.Token())
	assert.NotEqual(t, oldToken, target.Token())
	assert.NotEqual(t, untouchedToken, target.Token())
}

func TestScopedTokenWithoutCatalogCanReadCatalog(t *testing.T) {
	instance := startServer(t, "")
	status, headers, body := request(
		t, instance, http.MethodPost, tokensPath+"?nocatalog", scopedPasswordRequest, nil,
	)

	require.Equal(t, http.StatusCreated, status, "%s", body)
	tokenID := headers.Get("X-Subject-Token")
	require.NotEmpty(t, tokenID)
	var token struct {
		Fields map[string]json.RawMessage `json:"token"`
	}
	require.NoError(t, json.Unmarshal(body, &token))
	require.Contains(t, token.Fields, "project")
	assert.NotContains(t, token.Fields, "catalog")
	var project namedResource
	require.NoError(t, json.Unmarshal(token.Fields["project"], &project))
	require.NotEmpty(t, project.ID)

	status, _, body = request(t, instance, http.MethodGet, catalogPath, "", map[string]string{
		"X-Auth-Token": tokenID,
	})

	require.Equal(t, http.StatusOK, status, "%s", body)
	var result struct {
		Catalog []catalogService `json:"catalog"`
	}
	require.NoError(t, json.Unmarshal(body, &result))
	assertCatalog(t, result.Catalog, instance.URL, project.ID)
}

func TestHealthAndReadinessWithoutCredentials(t *testing.T) {
	instance := startServer(t, "")

	for _, path := range []string{"/_sandstack/health", "/_sandstack/ready"} {
		status, _, body := request(t, instance, http.MethodGet, path, "", nil)

		assert.Equal(t, http.StatusNoContent, status, path)
		assert.Empty(t, body, path)
	}
}

type namedResource struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Domain struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"domain"`
}

type catalogService struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	Endpoints []struct {
		ID        string `json:"id"`
		Interface string `json:"interface"`
		RegionID  string `json:"region_id"`
		URL       string `json:"url"`
	} `json:"endpoints"`
}

func assertResource(t *testing.T, resource namedResource, name string) {
	t.Helper()
	assert.Equal(t, name, resource.Name)
	assert.Equal(t, "default", resource.Domain.ID)
	assert.Equal(t, "Default", resource.Domain.Name)
}

func assertCatalog(t *testing.T, entries []catalogService, origin, projectID string) {
	t.Helper()
	expectedEndpoints := map[string]string{
		"identity":      origin + identityPath,
		"compute":       origin + computePath + "/" + projectID,
		"network":       origin + networkPath,
		"image":         origin + imagePath,
		"block-storage": origin + volumePath + "/" + projectID,
	}
	require.Len(t, entries, len(expectedEndpoints))
	for _, entry := range entries {
		require.Contains(t, expectedEndpoints, entry.Type)
		assert.NotEmpty(t, entry.ID)
		require.Len(t, entry.Endpoints, 1)
		endpoint := entry.Endpoints[0]
		assert.NotEmpty(t, endpoint.ID)
		assert.Equal(t, "public", endpoint.Interface)
		assert.Equal(t, "RegionOne", endpoint.RegionID)
		assert.Equal(t, expectedEndpoints[entry.Type], strings.TrimSuffix(endpoint.URL, "/"))
		delete(expectedEndpoints, entry.Type)
	}
}

func assertWireError(t *testing.T, body []byte, status int) {
	t.Helper()
	var result struct {
		Error struct {
			Code    int    `json:"code"`
			Title   string `json:"title"`
			Message string `json:"message"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(body, &result))
	assert.Equal(t, status, result.Error.Code)
	assert.Equal(t, http.StatusText(status), result.Error.Title)
	assert.NotEmpty(t, result.Error.Message)
}

func authenticate(t *testing.T, instance *httptest.Server) *gophercloud.ProviderClient {
	t.Helper()
	provider, err := openstack.AuthenticatedClient(t.Context(), authOptions(instance))
	require.NoError(t, err)
	require.NotEmpty(t, provider.Token())
	return provider
}

func authOptions(instance *httptest.Server) gophercloud.AuthOptions {
	return gophercloud.AuthOptions{
		IdentityEndpoint: instance.URL + identityPath,
		Username:         username,
		Password:         password,
		DomainName:       "Default",
		Scope: &gophercloud.AuthScope{
			ProjectName: projectName,
			DomainName:  "Default",
		},
	}
}

func identityClient(t *testing.T, provider *gophercloud.ProviderClient, origin string) *gophercloud.ServiceClient {
	t.Helper()
	client, err := openstack.NewIdentityV3(provider, gophercloud.EndpointOpts{
		Region:       "RegionOne",
		Availability: gophercloud.AvailabilityPublic,
	})
	require.NoError(t, err)
	assert.Equal(t, origin+identityPath+"/", client.Endpoint)
	return client
}

func checkClientCatalog(t *testing.T, client *gophercloud.ServiceClient, origin string) {
	t.Helper()
	page, err := catalog.List(client).AllPages(t.Context())
	require.NoError(t, err)
	entries, err := catalog.ExtractServiceCatalog(page)
	require.NoError(t, err)
	for _, entry := range entries {
		if entry.Type == "identity" {
			require.Len(t, entry.Endpoints, 1)
			assert.Equal(t, origin+identityPath, strings.TrimSuffix(entry.Endpoints[0].URL, "/"))
			return
		}
	}
	require.FailNow(t, "Identity service missing from catalog")
}

func assertClientUnauthorized(t *testing.T, client *gophercloud.ServiceClient) {
	t.Helper()
	_, err := catalog.List(client).AllPages(t.Context())
	require.Error(t, err)
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusUnauthorized), "%v", err)
}

func startServer(t *testing.T, publicURL string) *httptest.Server {
	t.Helper()
	handler, err := server.New(server.Config{
		Username:    username,
		Password:    password,
		ProjectName: projectName,
		AdminToken:  adminToken,
		PublicURL:   publicURL,
	})
	require.NoError(t, err)
	instance := httptest.NewServer(handler)
	t.Cleanup(instance.Close)
	return instance
}

func request(
	t *testing.T, instance *httptest.Server, method, path, body string, headers map[string]string,
) (int, http.Header, []byte) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, instance.URL+path, strings.NewReader(body))
	require.NoError(t, err)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	response, err := instance.Client().Do(req)
	require.NoError(t, err)
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	return response.StatusCode, response.Header, data
}

func requireJSON(t *testing.T, headers http.Header) {
	t.Helper()
	mediaType, _, err := mime.ParseMediaType(headers.Get("Content-Type"))
	require.NoError(t, err)
	require.Equal(t, "application/json", mediaType)
}
