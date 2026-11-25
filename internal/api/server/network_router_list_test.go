package server_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/layer3/routers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGophercloudListsRoutersByNameAndAdministrativeState(t *testing.T) {
	instance := startServer(t, "")
	client, disabled, enabled, decoy := createRouterListFixtures(t, authenticate(t, instance))
	adminDown := false
	for _, test := range []struct {
		name string
		opts routers.ListOptsBuilder
		want []routers.Router
	}{
		{"unfiltered", nil, []routers.Router{*disabled, *enabled, *decoy}},
		{"exact name", routers.ListOpts{Name: disabled.Name}, []routers.Router{*disabled, *enabled}},
		{"administratively down", routers.ListOpts{AdminStateUp: &adminDown}, []routers.Router{*disabled, *decoy}},
		{"both filters", routers.ListOpts{Name: disabled.Name, AdminStateUp: &adminDown}, []routers.Router{*disabled}},
		{"no match", routers.ListOpts{Name: "missing-router", AdminStateUp: &adminDown}, []routers.Router{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			pages, err := routers.List(client, test.opts).AllPages(t.Context())
			require.NoError(t, err)
			listed, err := routers.ExtractRouters(pages)
			require.NoError(t, err)
			assert.ElementsMatch(t, test.want, listed)
			for _, router := range listed {
				assert.Equal(t, "ACTIVE", router.Status)
			}
		})
	}
}

func TestRouterListFiltersWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	_, disabled, enabled, decoy := createRouterListFixtures(t, provider)
	auth := map[string]string{"X-Auth-Token": provider.Token()}

	// https://docs.openstack.org/api-ref/network/v2/#list-routers
	// https://docs.openstack.org/api-ref/network/v2/#filtering-and-column-selection
	for _, test := range []struct {
		name  string
		query url.Values
		want  []string
	}{
		{
			name:  "repeated exact names and disabled state",
			query: url.Values{"name": {disabled.Name, decoy.Name, disabled.Name}, "admin_state_up": {"off"}},
			want:  []string{disabled.ID, decoy.ID},
		},
		{
			name:  "repeated booleans and exact name",
			query: url.Values{"name": {disabled.Name}, "admin_state_up": {"false", "yes"}},
			want:  []string{disabled.ID, enabled.ID},
		},
		{
			name:  "name case is exact",
			query: url.Values{"name": {"MATCHED-ROUTER"}},
		},
		{
			name:  "explicit empty name matches no clients' routers",
			query: url.Values{"name": {""}},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			status, headers, body := request(t, instance, http.MethodGet, routersPath+"?"+test.query.Encode(), "", auth)
			require.Equal(t, http.StatusOK, status, "%s", body)
			requireJSON(t, headers)
			var response struct {
				Routers []wireRouter `json:"routers"`
			}
			require.NoError(t, json.Unmarshal(body, &response))
			require.NotNil(t, response.Routers)
			var ids []string
			for _, router := range response.Routers {
				ids = append(ids, router.ID)
				assert.Equal(t, "ACTIVE", router.Status)
				require.NotNil(t, router.AdminStateUp)
				assert.Equal(t, router.ID == enabled.ID, *router.AdminStateUp)
			}
			assert.ElementsMatch(t, test.want, ids)
		})
	}
	for _, bad := range []struct {
		query string
		kind  string
	}{
		{"admin_state_up=maybe", "InvalidInput"},
		{"admin_state_up=", "InvalidInput"},
		{"admin_state_up=false&admin_state_up=maybe", "InvalidInput"},
		{"status=ACTIVE", "BadRequest"},
	} {
		status, headers, body := request(t, instance, http.MethodGet, routersPath+"?"+bad.query, "", auth)
		require.Equal(t, http.StatusBadRequest, status, "%s: %s", bad.query, body)
		requireJSON(t, headers)
		assertPortNeutronError(t, body, bad.kind)
	}
}

func createRouterListFixtures(
	t *testing.T, provider *gophercloud.ProviderClient,
) (client *gophercloud.ServiceClient, disabled, enabled, decoy *routers.Router) {
	t.Helper()
	client, err := openstack.NewNetworkV2(provider, gophercloud.EndpointOpts{
		Region: "RegionOne", Availability: gophercloud.AvailabilityPublic,
	})
	require.NoError(t, err)
	create := func(name string, adminUp bool) *routers.Router {
		t.Helper()
		router, err := routers.Create(t.Context(), client, routers.CreateOpts{Name: name, AdminStateUp: &adminUp}).Extract()
		require.NoError(t, err)
		require.NotEmpty(t, router.ID)
		require.Equal(t, adminUp, router.AdminStateUp)
		return router
	}
	return client, create("matched-router", false), create("matched-router", true), create("another-router", false)
}
