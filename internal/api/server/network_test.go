package server_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/identity/v3/tokens"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/networks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	networkPath      = "/network"
	networksPath     = networkPath + "/v2.0/networks"
	fixtureNetworkID = "43d92fea-ce36-4d02-aba4-29806839f902"
)

func TestNetworkDiscoveryWire(t *testing.T) {
	const publicOrigin = "https://public.neutron.test"
	instance := startServer(t, publicOrigin)

	// https://docs.openstack.org/api-ref/network/v2/#list-api-versions
	status, headers, body := request(t, instance, http.MethodGet, networkPath+"/", "", nil)

	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	var discovery struct {
		Versions []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
			Links  []struct {
				Rel  string `json:"rel"`
				Href string `json:"href"`
			} `json:"links"`
		} `json:"versions"`
	}
	require.NoError(t, json.Unmarshal(body, &discovery))
	require.Len(t, discovery.Versions, 1)
	version := discovery.Versions[0]
	assert.Equal(t, "v2.0", version.ID)
	assert.Equal(t, "CURRENT", version.Status)
	require.Len(t, version.Links, 1)
	assert.Equal(t, "self", version.Links[0].Rel)
	assert.Equal(t, publicOrigin+networkPath+"/v2.0", strings.TrimSuffix(version.Links[0].Href, "/"))
}

func TestNetworkWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	token, ok := provider.GetAuthResult().(tokens.CreateResult)
	require.True(t, ok)
	project, err := token.ExtractProject()
	require.NoError(t, err)
	require.NotNil(t, project)
	require.NotEmpty(t, project.ID)
	auth := map[string]string{"X-Auth-Token": provider.Token()}

	// https://docs.openstack.org/api-ref/network/v2/#list-networks
	status, headers, body := request(t, instance, http.MethodGet, networksPath, "", auth)

	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	var list struct {
		Networks []wireNetwork `json:"networks"`
	}
	require.NoError(t, json.Unmarshal(body, &list))
	require.Len(t, list.Networks, 1)
	baseline := list.Networks[0]
	assert.Equal(t, fixtureNetworkID, baseline.ID)
	assert.Equal(t, "fixture-network", baseline.Name)
	assert.Equal(t, project.ID, baseline.ProjectID)
	assert.Equal(t, project.ID, baseline.TenantID)
	assert.Equal(t, "ACTIVE", baseline.Status)
	require.NotNil(t, baseline.AdminStateUp)
	assert.True(t, *baseline.AdminStateUp)
	require.NotNil(t, baseline.Shared)
	assert.False(t, *baseline.Shared)
	assert.Equal(t, []string{}, baseline.Subnets)

	// https://docs.openstack.org/api-ref/network/v2/#show-network-details
	detailPath := networksPath + "/" + fixtureNetworkID
	status, headers, body = request(t, instance, http.MethodGet, detailPath, "", auth)

	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	var detail struct {
		Network wireNetwork `json:"network"`
	}
	require.NoError(t, json.Unmarshal(body, &detail))
	assert.Equal(t, baseline, detail.Network)

	const missingID = "00000000-0000-0000-0000-000000000000"
	status, headers, body = request(t, instance, http.MethodGet, networksPath+"/"+missingID, "", auth)

	require.Equal(t, http.StatusNotFound, status, "%s", body)
	requireJSON(t, headers)
	var missing struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"NeutronError"`
	}
	require.NoError(t, json.Unmarshal(body, &missing))
	assert.Equal(t, "NetworkNotFound", missing.Error.Type)
	assert.NotEmpty(t, missing.Error.Message)

	status, _, body = request(t, instance, http.MethodGet, detailPath, "", nil)
	assert.Equal(t, http.StatusUnauthorized, status, "%s", body)
	status, _, body = request(t, instance, http.MethodGet, networksPath, "", map[string]string{
		"X-Auth-Token": "invalid-token",
	})
	assert.Equal(t, http.StatusUnauthorized, status, "%s", body)
	status, _, body = request(t, instance, http.MethodGet, networksPath+"?fields=id", "", auth)
	assert.Equal(t, http.StatusBadRequest, status, "%s", body)
}

func TestGophercloudDiscoversNetworkAndObservesFaultAndReset(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	discover := func(provider *gophercloud.ProviderClient) *gophercloud.ServiceClient {
		t.Helper()
		service, err := openstack.NewNetworkV2(provider, gophercloud.EndpointOpts{
			Region: "RegionOne", Availability: gophercloud.AvailabilityPublic,
		})
		require.NoError(t, err)
		assert.Equal(t, instance.URL+networkPath+"/", service.Endpoint)
		assert.Equal(t, instance.URL+networkPath+"/v2.0/", service.ResourceBase)
		return service
	}
	service := discover(provider)
	readList := func(client *gophercloud.ServiceClient) []networks.Network {
		t.Helper()
		pages, err := networks.List(client, nil).AllPages(t.Context())
		require.NoError(t, err)
		items, err := networks.ExtractNetworks(pages)
		require.NoError(t, err)
		return items
	}
	readDetail := func(client *gophercloud.ServiceClient, networkID string) *networks.Network {
		t.Helper()
		item, err := networks.Get(t.Context(), client, networkID).Extract()
		require.NoError(t, err)
		require.NotNil(t, item)
		return item
	}

	initial := readList(service)
	require.Len(t, initial, 1)
	baseline := initial[0]
	require.NotEmpty(t, baseline.ID)
	assert.Equal(t, "fixture-network", baseline.Name)
	assert.Equal(t, "ACTIVE", baseline.Status)
	assert.Equal(t, baseline, *readDetail(service, baseline.ID))

	faultPath := "/_sandstack/network/networks/" + baseline.ID + "/fault"
	status, _, body := request(t, instance, http.MethodPut, faultPath, "", map[string]string{
		"X-SandStack-Admin-Token": provider.Token(),
	})

	require.Equal(t, http.StatusUnauthorized, status, "%s", body)
	assert.Equal(t, baseline, *readDetail(service, baseline.ID))
	admin := map[string]string{"X-SandStack-Admin-Token": adminToken}
	status, _, body = request(t, instance, http.MethodPut, faultPath, "", admin)
	require.Equal(t, http.StatusNoContent, status, "%s", body)

	expectedDown := baseline
	expectedDown.Status = "DOWN"
	assert.Equal(t, []networks.Network{expectedDown}, readList(service))
	assert.Equal(t, expectedDown, *readDetail(service, baseline.ID))

	status, _, body = request(t, instance, http.MethodDelete, faultPath, "", admin)
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	assert.Equal(t, baseline, *readDetail(service, baseline.ID))
	status, _, body = request(t, instance, http.MethodPut, faultPath, "", admin)
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	assert.Equal(t, expectedDown, *readDetail(service, baseline.ID))

	status, _, body = request(t, instance, http.MethodPost, "/_sandstack/reset", "", admin)

	require.Equal(t, http.StatusNoContent, status, "%s", body)
	_, err := networks.Get(t.Context(), service, baseline.ID).Extract()
	require.Error(t, err)
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusUnauthorized), "%v", err)
	refreshed := discover(authenticate(t, instance))
	assert.Equal(t, baseline, *readDetail(refreshed, baseline.ID))
	assert.Equal(t, initial, readList(refreshed))
}

type wireNetwork struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	ProjectID    string   `json:"project_id"`
	TenantID     string   `json:"tenant_id"`
	Status       string   `json:"status"`
	AdminStateUp *bool    `json:"admin_state_up"`
	Shared       *bool    `json:"shared"`
	Subnets      []string `json:"subnets"`
}
