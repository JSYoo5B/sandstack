package server_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/identity/v3/tokens"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/layer3/routers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const routersPath = networkPath + "/v2.0/routers"

func TestPrivateRouterLifecycleWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	projectID := routerProjectID(t, provider)
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	readList := func() []wireRouter {
		t.Helper()
		status, headers, body := request(t, instance, http.MethodGet, routersPath, "", auth)
		require.Equal(t, http.StatusOK, status, "%s", body)
		requireJSON(t, headers)
		var response struct {
			Routers []wireRouter `json:"routers"`
		}
		require.NoError(t, json.Unmarshal(body, &response))
		require.NotNil(t, response.Routers)
		return response.Routers
	}
	assert.Empty(t, readList())
	const payload = `{"router":{"name":"wire-router","description":"private sandbox router"}}`
	status, _, body := request(t, instance, http.MethodPost, routersPath, payload, nil)
	assert.Equal(t, http.StatusUnauthorized, status, "%s", body)
	for _, invalid := range []struct {
		name string
		body string
	}{
		{"foreign project", `{"router":{"project_id":"00000000-0000-0000-0000-000000000000"}}`},
		{"foreign tenant", `{"router":{"tenant_id":"00000000-0000-0000-0000-000000000000"}}`},
		{"malformed external gateway", `{"router":{"external_gateway_info":{"network_id":"public"}}}`},
	} {
		t.Run(invalid.name, func(t *testing.T) {
			status, headers, body := request(t, instance, http.MethodPost, routersPath, invalid.body, auth)
			require.Equal(t, http.StatusBadRequest, status, "%s", body)
			requireJSON(t, headers)
		})
	}
	assert.Empty(t, readList())

	// https://docs.openstack.org/api-ref/network/v2/#create-router
	status, headers, body := request(t, instance, http.MethodPost, routersPath, payload, auth)
	require.Equal(t, http.StatusCreated, status, "%s", body)
	requireJSON(t, headers)
	var response, detail struct {
		Router wireRouter `json:"router"`
	}
	require.NoError(t, json.Unmarshal(body, &response))
	created := response.Router
	require.NotEmpty(t, created.ID)
	assert.Equal(t, projectID, created.ProjectID)
	assert.Equal(t, projectID, created.TenantID)
	assert.Equal(t, "wire-router", created.Name)
	assert.Equal(t, "private sandbox router", created.Description)
	assert.Equal(t, "ACTIVE", created.Status)
	require.NotNil(t, created.AdminStateUp)
	assert.True(t, *created.AdminStateUp)
	assert.JSONEq(t, "null", string(created.ExternalGatewayInfo))
	assert.Nil(t, created.GatewayPortID)
	assert.JSONEq(t, "[]", string(created.Routes))
	assert.Equal(t, []wireRouter{created}, readList())

	path := routersPath + "/" + created.ID
	status, headers, body = request(t, instance, http.MethodGet, path, "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	require.NoError(t, json.Unmarshal(body, &detail))
	assert.Equal(t, created, detail.Router)
	status, _, body = request(t, instance, http.MethodDelete, path, "", nil)
	assert.Equal(t, http.StatusUnauthorized, status, "%s", body)

	// https://docs.openstack.org/api-ref/network/v2/#delete-router
	status, _, body = request(t, instance, http.MethodDelete, path, "", auth)
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	assert.Empty(t, body)
	status, headers, body = request(t, instance, http.MethodGet, path, "", auth)
	require.Equal(t, http.StatusNotFound, status, "%s", body)
	requireJSON(t, headers)
	var missing struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"NeutronError"`
	}
	require.NoError(t, json.Unmarshal(body, &missing))
	assert.Equal(t, "RouterNotFound", missing.Error.Type)
	assert.NotEmpty(t, missing.Error.Message)
	assert.Empty(t, readList())
}

func TestGophercloudManagesPrivateRoutersAndObservesReset(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	projectID := routerProjectID(t, provider)
	endpoint := gophercloud.EndpointOpts{Region: "RegionOne", Availability: gophercloud.AvailabilityPublic}
	client, err := openstack.NewNetworkV2(provider, endpoint)
	require.NoError(t, err)
	list := func(service *gophercloud.ServiceClient) []routers.Router {
		t.Helper()
		pages, err := routers.List(service, nil).AllPages(t.Context())
		require.NoError(t, err)
		result, err := routers.ExtractRouters(pages)
		require.NoError(t, err)
		return result
	}
	create := func(opts routers.CreateOpts) *routers.Router {
		t.Helper()
		result, err := routers.Create(t.Context(), client, opts).Extract()
		require.NoError(t, err)
		require.NotNil(t, result)
		require.NotEmpty(t, result.ID)
		return result
	}
	assert.Empty(t, list(client))
	first := create(routers.CreateOpts{Name: "client-router"})
	adminUp := false
	second := create(routers.CreateOpts{
		Name: "disabled-router", Description: "administratively disabled", AdminStateUp: &adminUp,
		ProjectID: projectID, TenantID: projectID,
	})
	assert.NotEqual(t, first.ID, second.ID)
	assert.True(t, first.AdminStateUp)
	assert.False(t, second.AdminStateUp)
	for _, created := range []*routers.Router{first, second} {
		assert.Equal(t, projectID, created.ProjectID)
		assert.Equal(t, projectID, created.TenantID)
		// Neutron sets operational ACTIVE independently of the admin flag.
		// https://github.com/openstack/neutron/blob/master/neutron/db/l3_db.py#L238-L256
		assert.Equal(t, "ACTIVE", created.Status)
		assert.Equal(t, routers.GatewayInfo{}, created.GatewayInfo)
		assert.Equal(t, []routers.Route{}, created.Routes)
		read, err := routers.Get(t.Context(), client, created.ID).Extract()
		require.NoError(t, err)
		assert.Equal(t, created, read)
	}
	assert.ElementsMatch(t, []routers.Router{*first, *second}, list(client))

	require.NoError(t, routers.Delete(t.Context(), client, first.ID).ExtractErr())
	_, err = routers.Get(t.Context(), client, first.ID).Extract()
	require.Error(t, err)
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusNotFound), "%v", err)
	assert.Equal(t, []routers.Router{*second}, list(client))
	status, _, body := request(t, instance, http.MethodPost, "/_sandstack/reset", "", map[string]string{
		"X-SandStack-Admin-Token": adminToken,
	})
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	_, err = routers.Get(t.Context(), client, second.ID).Extract()
	require.Error(t, err)
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusUnauthorized), "%v", err)
	refreshed, err := openstack.NewNetworkV2(authenticate(t, instance), endpoint)
	require.NoError(t, err)
	assert.Empty(t, list(refreshed))
	_, err = routers.Get(t.Context(), refreshed, second.ID).Extract()
	require.Error(t, err)
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusNotFound), "%v", err)
}

func routerProjectID(t *testing.T, provider *gophercloud.ProviderClient) string {
	t.Helper()
	result, ok := provider.GetAuthResult().(tokens.CreateResult)
	require.True(t, ok)
	project, err := result.ExtractProject()
	require.NoError(t, err)
	require.NotNil(t, project)
	require.NotEmpty(t, project.ID)
	return project.ID
}

type wireRouter struct {
	ID                  string          `json:"id"`
	ProjectID           string          `json:"project_id"`
	TenantID            string          `json:"tenant_id"`
	Name                string          `json:"name"`
	Description         string          `json:"description"`
	Status              string          `json:"status"`
	AdminStateUp        *bool           `json:"admin_state_up"`
	ExternalGatewayInfo json.RawMessage `json:"external_gateway_info"`
	GatewayPortID       json.RawMessage `json:"gw_port_id"`
	Routes              json.RawMessage `json:"routes"`
}
