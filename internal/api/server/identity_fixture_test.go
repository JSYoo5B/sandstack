package server_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/volumes"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/servers"
	"github.com/gophercloud/gophercloud/v2/openstack/identity/v3/applicationcredentials"
	"github.com/gophercloud/gophercloud/v2/openstack/identity/v3/catalog"
	"github.com/gophercloud/gophercloud/v2/openstack/identity/v3/tokens"
	"github.com/gophercloud/gophercloud/v2/openstack/image/v2/images"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/networks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/JSYoo5B/sandstack/internal/api/server"
)

const (
	secondUsername    = "second-reader"
	secondPassword    = "second-test-password"
	secondProjectName = "second-test-project"
)

var secondFixture = server.Fixture{
	Username:    secondUsername,
	Password:    secondPassword,
	ProjectName: secondProjectName,
}

func startServerWithSecondFixture(t *testing.T, publicURL string) *httptest.Server {
	t.Helper()
	return startServer(t, publicURL, secondFixture)
}

func secondAuthOptions(instance *httptest.Server) gophercloud.AuthOptions {
	options := authOptions(instance)
	options.Username = secondUsername
	options.Password = secondPassword
	options.Scope = &gophercloud.AuthScope{ProjectName: secondProjectName, DomainName: "Default"}
	return options
}

func authenticateSecond(t *testing.T, instance *httptest.Server) *gophercloud.ProviderClient {
	t.Helper()
	provider, err := openstack.AuthenticatedClient(t.Context(), secondAuthOptions(instance))
	require.NoError(t, err)
	require.NotEmpty(t, provider.Token())
	return provider
}

func passwordRequest(user, secret, project string) string {
	return fmt.Sprintf(`{"auth":{
  "identity":{"methods":["password"],"password":{"user":{"name":%q,"domain":{"name":"Default"},"password":%q}}},
  "scope":{"project":{"name":%q,"domain":{"name":"Default"}}}
}}`, user, secret, project)
}

func TestServerConfigValidatesAdditionalFixtures(t *testing.T) {
	tests := map[string]server.Fixture{
		"missing username":          {Password: secondPassword, ProjectName: secondProjectName},
		"blank username":            {Username: " ", Password: secondPassword, ProjectName: secondProjectName},
		"missing password":          {Username: secondUsername, ProjectName: secondProjectName},
		"missing project":           {Username: secondUsername, Password: secondPassword},
		"admin token password":      {Username: secondUsername, Password: adminToken, ProjectName: secondProjectName},
		"primary username":          {Username: username, Password: secondPassword, ProjectName: secondProjectName},
		"primary project name":      {Username: secondUsername, Password: secondPassword, ProjectName: projectName},
		"repeated additional user":  secondFixture,
		"repeated additional scope": {Username: "third-reader", Password: "third", ProjectName: secondProjectName},
	}
	for name, fixture := range tests {
		t.Run(name, func(t *testing.T) {
			fixtures := []server.Fixture{fixture}
			if strings.HasPrefix(name, "repeated") {
				fixtures = []server.Fixture{secondFixture, fixture}
			}

			handler, err := server.New(testConfig("", fixtures...))

			assert.Error(t, err)
			assert.Nil(t, handler)
		})
	}

	handler, err := server.New(testConfig("", secondFixture, server.Fixture{
		Username: "third-reader", Password: password, ProjectName: "third-test-project",
	}))

	assert.NoError(t, err)
	assert.NotNil(t, handler)
}

func TestGophercloudAuthenticatesSeparateFixtures(t *testing.T) {
	instance := startServerWithSecondFixture(t, "")
	primary := authenticate(t, instance)
	second := authenticateSecond(t, instance)
	primaryResult, ok := primary.GetAuthResult().(tokens.CreateResult)
	require.True(t, ok)
	secondResult, ok := second.GetAuthResult().(tokens.CreateResult)
	require.True(t, ok)
	primaryProject, err := primaryResult.ExtractProject()
	require.NoError(t, err)
	secondProject, err := secondResult.ExtractProject()
	require.NoError(t, err)
	secondUser, err := secondResult.ExtractUser()
	require.NoError(t, err)

	assert.NotEqual(t, primary.Token(), second.Token())
	assert.Equal(t, projectName, primaryProject.Name)
	assert.Equal(t, secondProjectName, secondProject.Name)
	assert.Equal(t, secondUsername, secondUser.Name)
	assert.NotEqual(t, primaryProject.ID, secondProject.ID)

	endpoints := gophercloud.EndpointOpts{Region: "RegionOne", Availability: gophercloud.AvailabilityPublic}
	primaryVolumes, err := openstack.NewBlockStorageV3(primary, endpoints)
	require.NoError(t, err)
	secondVolumes, err := openstack.NewBlockStorageV3(second, endpoints)
	require.NoError(t, err)
	secondCompute, err := openstack.NewComputeV2(second, endpoints)
	require.NoError(t, err)
	secondImages, err := openstack.NewImageV2(second, endpoints)
	require.NoError(t, err)
	secondNetworks, err := openstack.NewNetworkV2(second, endpoints)
	require.NoError(t, err)

	assert.Equal(t, instance.URL+volumePath+"/"+primaryProject.ID+"/", primaryVolumes.Endpoint)
	assert.Equal(t, instance.URL+volumePath+"/"+secondProject.ID+"/", secondVolumes.Endpoint)
	assert.Equal(t, instance.URL+computePath+"/"+secondProject.ID+"/", secondCompute.Endpoint)

	page, err := catalog.List(identityClient(t, second, instance.URL)).AllPages(t.Context())
	require.NoError(t, err)
	listed, err := catalog.ExtractServiceCatalog(page)
	require.NoError(t, err)
	assertGophercloudCatalogProject(t, listed, instance.URL, secondProject.ID)

	primaryPages, err := volumes.List(primaryVolumes, volumes.ListOpts{}).AllPages(t.Context())
	require.NoError(t, err)
	primaryListed, err := volumes.ExtractVolumes(primaryPages)
	require.NoError(t, err)
	require.Len(t, primaryListed, 1)
	assert.Equal(t, fixtureVolumeID, primaryListed[0].ID)
	assertSecondProjectEmpty(t, secondVolumes, secondCompute, secondImages, secondNetworks)
}

func assertGophercloudCatalogProject(
	t *testing.T, entries []tokens.CatalogEntry, origin, projectID string,
) {
	t.Helper()
	urls := make(map[string]string, len(entries))
	for _, entry := range entries {
		require.Len(t, entry.Endpoints, 1)
		urls[entry.Type] = strings.TrimSuffix(entry.Endpoints[0].URL, "/")
	}
	assert.Equal(t, origin+computePath+"/"+projectID, urls["compute"])
	assert.Equal(t, origin+volumePath+"/"+projectID, urls["block-storage"])
}

func assertSecondProjectEmpty(
	t *testing.T, volumeClient, computeClient, imageClient, networkClient *gophercloud.ServiceClient,
) {
	t.Helper()
	volumePages, err := volumes.List(volumeClient, volumes.ListOpts{}).AllPages(t.Context())
	require.NoError(t, err)
	listedVolumes, err := volumes.ExtractVolumes(volumePages)
	require.NoError(t, err)
	assert.Empty(t, listedVolumes)

	serverPages, err := servers.List(computeClient, servers.ListOpts{}).AllPages(t.Context())
	require.NoError(t, err)
	listedServers, err := servers.ExtractServers(serverPages)
	require.NoError(t, err)
	assert.Empty(t, listedServers)

	imagePages, err := images.List(imageClient, images.ListOpts{}).AllPages(t.Context())
	require.NoError(t, err)
	listedImages, err := images.ExtractImages(imagePages)
	require.NoError(t, err)
	assert.Empty(t, listedImages)

	networkPages, err := networks.List(networkClient, networks.ListOpts{}).AllPages(t.Context())
	require.NoError(t, err)
	listedNetworks, err := networks.ExtractNetworks(networkPages)
	require.NoError(t, err)
	for _, network := range listedNetworks {
		assert.NotEqual(t, fixtureNetworkID, network.ID)
	}
}

func TestGophercloudInvalidatesEveryFixtureToken(t *testing.T) {
	instance := startServerWithSecondFixture(t, "")
	primaryClient := identityClient(t, authenticate(t, instance), instance.URL)
	secondClient := identityClient(t, authenticateSecond(t, instance), instance.URL)
	checkClientCatalog(t, primaryClient, instance.URL)
	checkClientCatalog(t, secondClient, instance.URL)

	status, _, body := request(t, instance, http.MethodPost, invalidatePath, "", map[string]string{
		"X-SandStack-Admin-Token": adminToken,
	})

	require.Equal(t, http.StatusNoContent, status, "%s", body)
	assertClientUnauthorized(t, primaryClient)
	assertClientUnauthorized(t, secondClient)
	checkClientCatalog(t, identityClient(t, authenticateSecond(t, instance), instance.URL), instance.URL)
}

func TestAdditionalFixtureTokenWire(t *testing.T) {
	const publicOrigin = "https://identity.example.test"
	instance := startServerWithSecondFixture(t, publicOrigin)
	primaryStatus, _, primaryBody := request(t, instance, http.MethodPost, tokensPath, scopedPasswordRequest, nil)
	require.Equal(t, http.StatusCreated, primaryStatus, "%s", primaryBody)
	var primary struct {
		Token struct {
			User    namedResource `json:"user"`
			Project namedResource `json:"project"`
		} `json:"token"`
	}
	require.NoError(t, json.Unmarshal(primaryBody, &primary))

	// OpenStack Identity v3 password authentication with scoped authorization.
	// https://docs.openstack.org/api-ref/identity/v3/?expanded=password-authentication-with-scoped-authorization-detail
	status, headers, body := request(t, instance, http.MethodPost, tokensPath,
		passwordRequest(secondUsername, secondPassword, secondProjectName), nil)

	require.Equal(t, http.StatusCreated, status, "%s", body)
	tokenID := headers.Get("X-Subject-Token")
	require.NotEmpty(t, tokenID)
	var result struct {
		Token struct {
			Methods []string         `json:"methods"`
			User    namedResource    `json:"user"`
			Project namedResource    `json:"project"`
			Roles   []namedResource  `json:"roles"`
			Catalog []catalogService `json:"catalog"`
		} `json:"token"`
	}
	require.NoError(t, json.Unmarshal(body, &result))
	issued := result.Token
	assert.Equal(t, []string{"password"}, issued.Methods)
	assertResource(t, issued.User, secondUsername)
	assertResource(t, issued.Project, secondProjectName)
	assert.Len(t, issued.User.ID, 32)
	assert.Len(t, issued.Project.ID, 32)
	assert.NotEqual(t, primary.Token.User.ID, issued.User.ID)
	assert.NotEqual(t, primary.Token.Project.ID, issued.Project.ID)
	require.Len(t, issued.Roles, 1)
	assert.Equal(t, "member", issued.Roles[0].Name)
	assertCatalog(t, issued.Catalog, publicOrigin, issued.Project.ID)

	restarted := startServerWithSecondFixture(t, publicOrigin)
	status, _, body = request(t, restarted, http.MethodPost, tokensPath,
		passwordRequest(secondUsername, secondPassword, secondProjectName), nil)
	require.Equal(t, http.StatusCreated, status, "%s", body)
	var again struct {
		Token struct {
			User    namedResource `json:"user"`
			Project namedResource `json:"project"`
		} `json:"token"`
	}
	require.NoError(t, json.Unmarshal(body, &again))
	assert.Equal(t, issued.User, again.Token.User)
	assert.Equal(t, issued.Project, again.Token.Project)

	// https://docs.openstack.org/api-ref/identity/v3/?expanded=get-service-catalog-detail
	status, _, body = request(t, instance, http.MethodGet, catalogPath, "", map[string]string{
		"X-Auth-Token": tokenID,
	})

	require.Equal(t, http.StatusOK, status, "%s", body)
	var listing struct {
		Catalog []catalogService `json:"catalog"`
	}
	require.NoError(t, json.Unmarshal(body, &listing))
	assert.Equal(t, issued.Catalog, listing.Catalog)

	// Keystone mints a project-scoped token only when the user has a role on
	// the project, and Unauthorized hides its detail outside insecure_debug.
	// https://github.com/openstack/keystone/blob/ffeba71ada0492f95c0e370b65d324005130000b/keystone/models/token_model.py#L543-L550
	// https://github.com/openstack/keystone/blob/ffeba71ada0492f95c0e370b65d324005130000b/keystone/exception.py#L298-L317
	// https://github.com/openstack/keystone/blob/ffeba71ada0492f95c0e370b65d324005130000b/keystone/server/flask/application.py#L158-L172
	refusals := map[string]string{
		"primary user on second project by name": passwordRequest(username, password, secondProjectName),
		"second user on primary project by name": passwordRequest(secondUsername, secondPassword, projectName),
		"primary user on second project by ID": fmt.Sprintf(`{"auth":{
  "identity":{"methods":["password"],"password":{"user":{"id":%q,"password":%q}}},
  "scope":{"project":{"id":%q}}
}}`, primary.Token.User.ID, password, issued.Project.ID),
		"second user on primary project by ID": fmt.Sprintf(`{"auth":{
  "identity":{"methods":["password"],"password":{"user":{"id":%q,"password":%q}}},
  "scope":{"project":{"id":%q}}
}}`, issued.User.ID, secondPassword, primary.Token.Project.ID),
	}
	for name, refused := range refusals {
		t.Run(name, func(t *testing.T) {
			status, headers, body := request(t, instance, http.MethodPost, tokensPath, refused, nil)

			require.Equal(t, http.StatusUnauthorized, status, "%s", body)
			requireJSON(t, headers)
			assert.Empty(t, headers.Get("X-Subject-Token"))
			assert.Equal(t, `Keystone uri="`+publicOrigin+identityPath+`"`, headers.Get("WWW-Authenticate"))
			assert.JSONEq(t, `{"error":{
  "code":401,
  "title":"Unauthorized",
  "message":"The request you have made requires authentication."
}}`, string(body))
		})
	}
}

func TestAdditionalFixtureCannotReachPrimaryResources(t *testing.T) {
	instance := startServerWithSecondFixture(t, "")
	primary := authenticate(t, instance)
	second := authenticateSecond(t, instance)
	primaryProjectID := volumeProjectID(t, primary)
	secondProjectID := volumeProjectID(t, second)
	auth := map[string]string{"X-Auth-Token": second.Token()}

	status, _, body := request(t, instance, http.MethodGet,
		volumePath+"/"+primaryProjectID+"/volumes/"+fixtureVolumeID, "", auth)

	assert.Equal(t, http.StatusBadRequest, status, "%s", body)

	hidden := map[string]string{
		"volume":  volumePath + "/" + secondProjectID + "/volumes/" + fixtureVolumeID,
		"server":  computePath + "/" + secondProjectID + "/servers/" + fixtureServerID,
		"image":   imagesPath + "/" + fixtureImageID,
		"network": networksPath + "/" + fixtureNetworkID,
	}
	for name, path := range hidden {
		t.Run(name, func(t *testing.T) {
			status, _, body := request(t, instance, http.MethodGet, path, "", auth)

			assert.Equal(t, http.StatusNotFound, status, "%s", body)

			status, _, body = request(t, instance, http.MethodDelete, path, "", auth)

			assert.Equal(t, http.StatusNotFound, status, "%s", body)
		})
	}

	primaryAuth := map[string]string{"X-Auth-Token": primary.Token()}
	for name, path := range map[string]string{
		"volume":  volumePath + "/" + primaryProjectID + "/volumes/" + fixtureVolumeID,
		"server":  computePath + "/" + primaryProjectID + "/servers/" + fixtureServerID,
		"image":   imagesPath + "/" + fixtureImageID,
		"network": networksPath + "/" + fixtureNetworkID,
	} {
		status, _, body := request(t, instance, http.MethodGet, path, "", primaryAuth)

		assert.Equal(t, http.StatusOK, status, "%s %s", name, body)
	}
}

func TestGophercloudApplicationCredentialsFollowTheirFixture(t *testing.T) {
	instance := startServerWithSecondFixture(t, "")
	primary := authenticate(t, instance)
	second := authenticateSecond(t, instance)
	primaryClient := identityClient(t, primary, instance.URL)
	secondClient := identityClient(t, second, instance.URL)
	secondResult, ok := second.GetAuthResult().(tokens.CreateResult)
	require.True(t, ok)
	secondUser, err := secondResult.ExtractUser()
	require.NoError(t, err)
	secondProject, err := secondResult.ExtractProject()
	require.NoError(t, err)

	created, err := applicationcredentials.Create(t.Context(), secondClient, secondUser.ID,
		applicationcredentials.CreateOpts{Name: "second-job"}).Extract()

	require.NoError(t, err)
	assert.Equal(t, secondProject.ID, created.ProjectID)

	_, err = applicationcredentials.List(primaryClient, secondUser.ID, nil).AllPages(t.Context())
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusForbidden), "%v", err)
	err = applicationcredentials.Delete(t.Context(), primaryClient, secondUser.ID, created.ID).ExtractErr()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusForbidden), "%v", err)

	options := gophercloud.AuthOptions{
		IdentityEndpoint:            instance.URL + identityPath,
		ApplicationCredentialID:     created.ID,
		ApplicationCredentialSecret: created.Secret,
	}
	job, err := openstack.AuthenticatedClient(t.Context(), options)
	require.NoError(t, err)
	jobResult, ok := job.GetAuthResult().(tokens.CreateResult)
	require.True(t, ok)
	jobProject, err := jobResult.ExtractProject()
	require.NoError(t, err)
	jobUser, err := jobResult.ExtractUser()
	require.NoError(t, err)
	assert.Equal(t, secondProject.ID, jobProject.ID)
	assert.Equal(t, secondUser.ID, jobUser.ID)
	jobVolumes, err := openstack.NewBlockStorageV3(job, gophercloud.EndpointOpts{Region: "RegionOne"})
	require.NoError(t, err)
	assert.Equal(t, instance.URL+volumePath+"/"+secondProject.ID+"/", jobVolumes.Endpoint)
}
