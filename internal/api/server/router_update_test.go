package server_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/layer3/routers"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/ports"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGophercloudUpdatesRouterWithAttachedInterface(t *testing.T) {
	instance := startServer(t, "")
	resources := createRouterInterfaceResources(t, authenticate(t, instance))
	ctx := t.Context()
	service := resources.service
	routerID := resources.routerID
	initial, err := routers.Get(ctx, service, routerID).Extract()
	require.NoError(t, err)
	require.True(t, initial.AdminStateUp)
	added, err := routers.AddInterface(ctx, service, routerID, routers.AddInterfaceOpts{
		SubnetID: resources.subnetID,
	}).Extract()
	require.NoError(t, err)
	port, err := ports.Get(ctx, service, added.PortID).Extract()
	require.NoError(t, err)
	require.Equal(t, routerID, port.DeviceID)

	description := "internal interface survives an administrative change"
	revised, err := routers.Update(ctx, service, routerID, routers.UpdateOpts{
		Name: "renamed-router", Description: &description,
	}).Extract()

	require.NoError(t, err)
	want := *initial
	want.Name = "renamed-router"
	want.Description = description
	assert.Equal(t, &want, revised)

	adminUp := false
	revised, err = routers.Update(ctx, service, routerID, routers.UpdateOpts{AdminStateUp: &adminUp}).Extract()

	require.NoError(t, err)
	want.AdminStateUp = false
	assert.Equal(t, &want, revised)

	emptyDescription := ""
	revised, err = routers.Update(ctx, service, routerID, routers.UpdateOpts{Description: &emptyDescription}).Extract()

	require.NoError(t, err)
	want.Description = ""
	assert.Equal(t, &want, revised)
	detail, err := routers.Get(ctx, service, routerID).Extract()
	require.NoError(t, err)
	assert.Equal(t, &want, detail)
	pages, err := routers.List(service, nil).AllPages(ctx)
	require.NoError(t, err)
	listed, err := routers.ExtractRouters(pages)
	require.NoError(t, err)
	assert.Equal(t, []routers.Router{want}, listed)
	remaining, err := ports.Get(ctx, service, port.ID).Extract()
	require.NoError(t, err)
	assert.Equal(t, port, remaining)
	err = routers.Delete(ctx, service, routerID).ExtractErr()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusConflict), "delete router with interface: %v", err)
}

func TestRouterUpdateWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	projectID := routerProjectID(t, provider)
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	status, _, body := request(t, instance, http.MethodPost, routersPath,
		`{"router":{"name":"original","description":"preserve this","admin_state_up":true}}`, auth)
	require.Equal(t, http.StatusCreated, status, "%s", body)
	var response struct {
		Router wireRouter `json:"router"`
	}
	require.NoError(t, json.Unmarshal(body, &response))
	require.NotEmpty(t, response.Router.ID)
	path := routersPath + "/" + response.Router.ID

	// https://docs.openstack.org/api-ref/network/v2/#update-router
	status, headers, body := request(t, instance, http.MethodPut, path, `{"router":{"admin_state_up":false}}`, auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	var afterAdminChange struct {
		Router wireRouter `json:"router"`
	}
	require.NoError(t, json.Unmarshal(body, &afterAdminChange))
	assert.Equal(t, response.Router.ID, afterAdminChange.Router.ID)
	assert.Equal(t, projectID, afterAdminChange.Router.ProjectID)
	assert.Equal(t, projectID, afterAdminChange.Router.TenantID)
	assert.Equal(t, "original", afterAdminChange.Router.Name)
	assert.Equal(t, "preserve this", afterAdminChange.Router.Description)
	require.NotNil(t, afterAdminChange.Router.AdminStateUp)
	assert.False(t, *afterAdminChange.Router.AdminStateUp)
	assert.Equal(t, "ACTIVE", afterAdminChange.Router.Status)
	assert.JSONEq(t, "null", string(afterAdminChange.Router.ExternalGatewayInfo))
	assert.JSONEq(t, "null", string(afterAdminChange.Router.GatewayPortID))
	assert.JSONEq(t, "[]", string(afterAdminChange.Router.Routes))

	clearText := `{"router":{"name":"","description":""}}`
	status, _, body = request(t, instance, http.MethodPut, path, clearText, nil)
	require.Equal(t, http.StatusUnauthorized, status, "%s", body)
	status, headers, body = request(t, instance, http.MethodPut, path, clearText, auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	var cleared struct {
		Router map[string]json.RawMessage `json:"router"`
	}
	require.NoError(t, json.Unmarshal(body, &cleared))
	assert.JSONEq(t, `""`, string(cleared.Router["name"]))
	assert.JSONEq(t, `""`, string(cleared.Router["description"]))
	assert.JSONEq(t, "false", string(cleared.Router["admin_state_up"]))
	clearedJSON := string(body)
	status, headers, body = request(t, instance, http.MethodPut, path, `{"router":{}}`, auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	assert.JSONEq(t, clearedJSON, string(body))

	for _, invalid := range []struct {
		name string
		body string
	}{
		{"null name", `{"router":{"name":null}}`},
		{"null description", `{"router":{"description":null}}`},
		{"null administrative state", `{"router":{"admin_state_up":null}}`},
		{"external gateway", `{"router":{"external_gateway_info":{"network_id":"00000000-0000-0000-0000-000000000000"}}}`},
		{"unexpected field", `{"router":{"name":"must not be applied","unexpected":true}}`},
		{"own project field", fmt.Sprintf(`{"router":{"project_id":%q}}`, projectID)},
		{"own tenant field", fmt.Sprintf(`{"router":{"tenant_id":%q}}`, projectID)},
	} {
		t.Run(invalid.name, func(t *testing.T) {
			status, headers, body := request(t, instance, http.MethodPut, path, invalid.body, auth)
			require.Equal(t, http.StatusBadRequest, status, "%s", body)
			requireJSON(t, headers)
			assertPortNeutronError(t, body, "BadRequest")
		})
	}
	status, headers, body = request(t, instance, http.MethodGet, path, "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	assert.JSONEq(t, clearedJSON, string(body))

	const missingID = "00000000-0000-0000-0000-000000000000"
	status, headers, body = request(t, instance, http.MethodPut, routersPath+"/"+missingID,
		`{"router":{"name":"unknown router"}}`, auth)
	require.Equal(t, http.StatusNotFound, status, "%s", body)
	requireJSON(t, headers)
	assertPortNeutronError(t, body, "RouterNotFound")
}
