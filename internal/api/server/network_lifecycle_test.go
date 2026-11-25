package server_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/identity/v3/tokens"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/networks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNetworkCreateAndDeleteWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	result, ok := provider.GetAuthResult().(tokens.CreateResult)
	require.True(t, ok)
	project, err := result.ExtractProject()
	require.NoError(t, err)
	require.NotNil(t, project)
	require.NotEmpty(t, project.ID)
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	readList := func() []wireNetwork {
		t.Helper()
		status, headers, body := request(t, instance, http.MethodGet, networksPath, "", auth)
		require.Equal(t, http.StatusOK, status, "%s", body)
		requireJSON(t, headers)
		var response struct {
			Networks []wireNetwork `json:"networks"`
		}
		require.NoError(t, json.Unmarshal(body, &response))
		return response.Networks
	}
	baseline := readList()
	require.Len(t, baseline, 1)

	const enabledBody = `{"network":{"name":"wire-network"}}`
	status, _, body := request(t, instance, http.MethodPost, networksPath, enabledBody, nil)
	assert.Equal(t, http.StatusUnauthorized, status, "%s", body)

	for _, invalid := range []struct {
		name       string
		body       string
		wantStatus int
	}{
		{
			name:       "foreign project",
			body:       `{"network":{"project_id":"00000000-0000-0000-0000-000000000000"}}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "foreign tenant",
			body:       `{"network":{"tenant_id":"00000000-0000-0000-0000-000000000000"}}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "unsupported field",
			body:       `{"network":{"router:external":true}}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "shared resource",
			body:       `{"network":{"shared":true}}`,
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "incorrect value type",
			body:       `{"network":{"admin_state_up":"false"}}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "duplicate field",
			body:       `{"network":{"name":"first","name":"second"}}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "trailing document",
			body:       `{"network":{"name":"first"}} {"network":{"name":"second"}}`,
			wantStatus: http.StatusBadRequest,
		},
	} {
		t.Run(invalid.name, func(t *testing.T) {
			status, headers, body := request(t, instance, http.MethodPost, networksPath, invalid.body, auth)

			require.Equal(t, invalid.wantStatus, status, "%s", body)
			requireJSON(t, headers)
			var response struct {
				Error struct {
					Type    string `json:"type"`
					Message string `json:"message"`
				} `json:"NeutronError"`
			}
			require.NoError(t, json.Unmarshal(body, &response))
			assert.NotEmpty(t, response.Error.Type)
			assert.NotEmpty(t, response.Error.Message)
		})
	}
	assert.Equal(t, baseline, readList())

	// https://docs.openstack.org/api-ref/network/v2/#create-network
	for _, example := range []struct {
		body    string
		name    string
		status  string
		adminUp bool
	}{
		{status: "ACTIVE", adminUp: true},
		{body: `{"network":{}}`, status: "ACTIVE", adminUp: true},
		{body: enabledBody, name: "wire-network", status: "ACTIVE", adminUp: true},
		{
			body: fmt.Sprintf(`{"network":{"name":"disabled-network","admin_state_up":false,"shared":false,"project_id":%q,"tenant_id":%q}}`, project.ID, project.ID),
			name: "disabled-network", status: "DOWN", adminUp: false,
		},
	} {
		status, headers, body := request(t, instance, http.MethodPost, networksPath, example.body, auth)

		require.Equal(t, http.StatusCreated, status, "%s", body)
		requireJSON(t, headers)
		var response struct {
			Network wireNetwork `json:"network"`
		}
		require.NoError(t, json.Unmarshal(body, &response))
		created := response.Network
		require.NotEmpty(t, created.ID)
		assert.NotEqual(t, fixtureNetworkID, created.ID)
		assert.Equal(t, example.name, created.Name)
		assert.Equal(t, project.ID, created.ProjectID)
		assert.Equal(t, project.ID, created.TenantID)
		assert.Equal(t, example.status, created.Status)
		require.NotNil(t, created.AdminStateUp)
		assert.Equal(t, example.adminUp, *created.AdminStateUp)
		require.NotNil(t, created.Shared)
		assert.False(t, *created.Shared)
		assert.Equal(t, []string{}, created.Subnets)

		detail := networksPath + "/" + created.ID
		status, _, body = request(t, instance, http.MethodDelete, detail, "", nil)
		assert.Equal(t, http.StatusUnauthorized, status, "%s", body)

		// https://docs.openstack.org/api-ref/network/v2/#delete-network
		status, _, body = request(t, instance, http.MethodDelete, detail, "", auth)

		require.Equal(t, http.StatusNoContent, status, "%s", body)
		assert.Empty(t, body)
		status, _, body = request(t, instance, http.MethodGet, detail, "", auth)
		assert.Equal(t, http.StatusNotFound, status, "%s", body)
	}
	assert.Equal(t, baseline, readList())
}

func TestGophercloudCreatesFaultsDeletesAndResetsNetworks(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	discover := func(provider *gophercloud.ProviderClient) *gophercloud.ServiceClient {
		t.Helper()
		service, err := openstack.NewNetworkV2(provider, gophercloud.EndpointOpts{
			Region: "RegionOne", Availability: gophercloud.AvailabilityPublic,
		})
		require.NoError(t, err)
		return service
	}
	readList := func(client *gophercloud.ServiceClient) []networks.Network {
		t.Helper()
		pages, err := networks.List(client, nil).AllPages(t.Context())
		require.NoError(t, err)
		items, err := networks.ExtractNetworks(pages)
		require.NoError(t, err)
		return items
	}
	service := discover(provider)
	baseline := readList(service)
	require.Len(t, baseline, 1)
	require.Equal(t, fixtureNetworkID, baseline[0].ID)

	created, err := networks.Create(t.Context(), service, networks.CreateOpts{Name: "sdk-network"}).Extract()

	require.NoError(t, err)
	require.NotNil(t, created)
	require.NotEmpty(t, created.ID)
	assert.NotEqual(t, fixtureNetworkID, created.ID)
	assert.Equal(t, "sdk-network", created.Name)
	assert.Equal(t, baseline[0].ProjectID, created.ProjectID)
	assert.True(t, created.AdminStateUp)
	assert.Equal(t, "ACTIVE", created.Status)
	read, err := networks.Get(t.Context(), service, created.ID).Extract()
	require.NoError(t, err)
	assert.Equal(t, created, read)
	assert.ElementsMatch(t, []networks.Network{baseline[0], *created}, readList(service))

	admin := map[string]string{"X-SandStack-Admin-Token": adminToken}
	faultPath := "/_sandstack/network/networks/" + created.ID + "/fault"
	status, _, body := request(t, instance, http.MethodPut, faultPath, "", admin)
	require.Equal(t, http.StatusNoContent, status, "%s", body)

	down := *created
	down.Status = "DOWN"
	read, err = networks.Get(t.Context(), service, created.ID).Extract()
	require.NoError(t, err)
	assert.Equal(t, down, *read)
	assert.ElementsMatch(t, []networks.Network{baseline[0], down}, readList(service))

	require.NoError(t, networks.Delete(t.Context(), service, created.ID).ExtractErr())
	_, err = networks.Get(t.Context(), service, created.ID).Extract()
	require.Error(t, err)
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusNotFound), "%v", err)
	assert.Equal(t, baseline, readList(service))

	remaining, err := networks.Create(t.Context(), service, networks.CreateOpts{Name: "reset-network"}).Extract()
	require.NoError(t, err)
	require.NotNil(t, remaining)
	require.NotEmpty(t, remaining.ID)
	assert.NotEqual(t, created.ID, remaining.ID)
	faultPath = "/_sandstack/network/networks/" + baseline[0].ID + "/fault"
	status, _, body = request(t, instance, http.MethodPut, faultPath, "", admin)
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	seedDown := baseline[0]
	seedDown.Status = "DOWN"
	assert.ElementsMatch(t, []networks.Network{seedDown, *remaining}, readList(service))

	status, _, body = request(t, instance, http.MethodPost, "/_sandstack/reset", "", admin)

	require.Equal(t, http.StatusNoContent, status, "%s", body)
	_, err = networks.Get(t.Context(), service, fixtureNetworkID).Extract()
	require.Error(t, err)
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusUnauthorized), "%v", err)
	current := discover(authenticate(t, instance))
	assert.Equal(t, baseline, readList(current))
	_, err = networks.Get(t.Context(), current, remaining.ID).Extract()
	require.Error(t, err)
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusNotFound), "%v", err)
}
