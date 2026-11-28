package server_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/external"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/layer3/floatingips"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/layer3/routers"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/networks"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/ports"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/subnets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const floatingIPsPath = networkPath + "/v2.0/floatingips"

type floatingIPResources struct {
	routerInterfaceResources
	externalNetworkID string
	portID            string
	fixedIP           string
}

func createFloatingIPResources(t *testing.T, provider *gophercloud.ProviderClient) floatingIPResources {
	t.Helper()
	resources := createRouterInterfaceResources(t, provider)
	ctx := t.Context()
	isExternal := true
	pages, err := networks.List(resources.service, external.ListOptsExt{
		ListOptsBuilder: networks.ListOpts{}, External: &isExternal,
	}).AllPages(ctx)
	require.NoError(t, err)
	discovered, err := networks.ExtractNetworks(pages)
	require.NoError(t, err)
	require.Len(t, discovered, 1)
	gateway := routers.GatewayInfo{NetworkID: discovered[0].ID}
	_, err = routers.Update(ctx, resources.service, resources.routerID, routers.UpdateOpts{GatewayInfo: &gateway}).Extract()
	require.NoError(t, err)
	_, err = routers.AddInterface(ctx, resources.service, resources.routerID, routers.AddInterfaceOpts{
		SubnetID: resources.subnetID,
	}).Extract()
	require.NoError(t, err)
	port, err := ports.Create(ctx, resources.service, ports.CreateOpts{NetworkID: resources.networkID}).Extract()
	require.NoError(t, err)
	require.Len(t, port.FixedIPs, 1)
	return floatingIPResources{
		routerInterfaceResources: resources,
		externalNetworkID:        discovered[0].ID,
		portID:                   port.ID,
		fixedIP:                  port.FixedIPs[0].IPAddress,
	}
}

func TestGophercloudAssociatesFloatingIPWithServerPort(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	resources := createFloatingIPResources(t, provider)
	client := resources.service
	ctx := t.Context()
	projectID := routerProjectID(t, provider)

	// https://docs.openstack.org/api-ref/network/v2/#create-floating-ip
	created, err := floatingips.Create(ctx, client, floatingips.CreateOpts{
		FloatingNetworkID: resources.externalNetworkID, PortID: resources.portID, Description: "web",
	}).Extract()
	require.NoError(t, err)
	read, err := floatingips.Get(ctx, client, created.ID).Extract()
	require.NoError(t, err)
	pages, err := floatingips.List(client, floatingips.ListOpts{PortID: resources.portID}).AllPages(ctx)
	require.NoError(t, err)
	listed, err := floatingips.ExtractFloatingIPs(pages)
	require.NoError(t, err)

	assert.Equal(t, floatingips.FloatingIP{
		ID:                created.ID,
		Description:       "web",
		FloatingNetworkID: resources.externalNetworkID,
		FloatingIP:        "203.0.113.3",
		PortID:            resources.portID,
		FixedIP:           resources.fixedIP,
		TenantID:          projectID,
		ProjectID:         projectID,
		Status:            "DOWN",
		RouterID:          resources.routerID,
		Tags:              []string{},
	}, *created)
	active := *created
	active.Status = "ACTIVE"
	assert.Equal(t, active, *read)
	assert.Equal(t, []floatingips.FloatingIP{active}, listed)

	// https://docs.openstack.org/api-ref/network/v2/#update-floating-ip
	empty := ""
	disassociated, err := floatingips.Update(ctx, client, created.ID, floatingips.UpdateOpts{PortID: &empty}).Extract()
	require.NoError(t, err)
	idle, err := floatingips.Get(ctx, client, created.ID).Extract()
	require.NoError(t, err)

	assert.Empty(t, disassociated.PortID)
	assert.Empty(t, disassociated.FixedIP)
	assert.Empty(t, disassociated.RouterID)
	assert.Equal(t, "ACTIVE", disassociated.Status)
	assert.Equal(t, "DOWN", idle.Status)

	reassociated, err := floatingips.Update(ctx, client, created.ID, floatingips.UpdateOpts{
		PortID: &resources.portID, FixedIP: resources.fixedIP,
	}).Extract()
	require.NoError(t, err)
	assert.Equal(t, resources.routerID, reassociated.RouterID)
	assert.Equal(t, "DOWN", reassociated.Status)

	_, err = subnets.Create(ctx, client, subnets.CreateOpts{
		NetworkID: fixtureNetworkID, CIDR: "10.77.0.0/24", IPVersion: gophercloud.IPv4,
	}).Extract()
	require.NoError(t, err)
	unreachable, err := ports.Create(ctx, client, ports.CreateOpts{NetworkID: fixtureNetworkID}).Extract()
	require.NoError(t, err)
	for _, tc := range []struct {
		name   string
		opts   floatingips.CreateOpts
		status int
	}{
		{
			name:   "missing external network",
			opts:   floatingips.CreateOpts{FloatingNetworkID: missingNetworkMarker},
			status: http.StatusNotFound,
		},
		{
			name:   "private network",
			opts:   floatingips.CreateOpts{FloatingNetworkID: resources.networkID},
			status: http.StatusBadRequest,
		},
		{
			name:   "port without a router path",
			opts:   floatingips.CreateOpts{FloatingNetworkID: resources.externalNetworkID, PortID: unreachable.ID},
			status: http.StatusNotFound,
		},
		{
			name:   "fixed IP already associated",
			opts:   floatingips.CreateOpts{FloatingNetworkID: resources.externalNetworkID, PortID: resources.portID},
			status: http.StatusConflict,
		},
		{
			name: "requested address",
			opts: floatingips.CreateOpts{
				FloatingNetworkID: resources.externalNetworkID, FloatingIP: "203.0.113.100",
			},
			status: http.StatusForbidden,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := floatingips.Create(ctx, client, tc.opts).Extract()

			assert.True(t, gophercloud.ResponseCodeIs(err, tc.status), "%v", err)
		})
	}

	_, err = routers.RemoveInterface(ctx, client, resources.routerID, routers.RemoveInterfaceOpts{
		SubnetID: resources.subnetID,
	}).Extract()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusConflict), "%v", err)
	_, err = routers.Update(ctx, client, resources.routerID, routers.UpdateOpts{
		GatewayInfo: &routers.GatewayInfo{},
	}).Extract()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusConflict), "%v", err)

	require.NoError(t, ports.Delete(ctx, client, resources.portID).ExtractErr())
	released, err := floatingips.Get(ctx, client, created.ID).Extract()
	require.NoError(t, err)

	assert.Empty(t, released.PortID)
	assert.Empty(t, released.RouterID)
	assert.Equal(t, "DOWN", released.Status)

	// https://docs.openstack.org/api-ref/network/v2/#delete-floating-ip
	require.NoError(t, floatingips.Delete(ctx, client, created.ID).ExtractErr())
	_, err = floatingips.Get(ctx, client, created.ID).Extract()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusNotFound), "%v", err)
	_, err = routers.Update(ctx, client, resources.routerID, routers.UpdateOpts{
		GatewayInfo: &routers.GatewayInfo{},
	}).Extract()
	assert.NoError(t, err)
}

func TestFloatingIPWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	resources := createFloatingIPResources(t, provider)
	projectID := routerProjectID(t, provider)
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	admin := map[string]string{"X-SandStack-Admin-Token": adminToken}
	send := func(method, path, body string) (int, []byte) {
		t.Helper()
		status, headers, response := request(t, instance, method, path, body, auth)
		requireJSON(t, headers)
		return status, response
	}
	createBody := fmt.Sprintf(`{"floatingip":{"floating_network_id":%q,"port_id":%q,"project_id":%q}}`,
		resources.externalNetworkID, resources.portID, projectID)

	status, _, body := request(t, instance, http.MethodPost, floatingIPsPath, createBody, nil)
	require.Equal(t, http.StatusUnauthorized, status, "%s", body)
	for _, tc := range []struct {
		name   string
		body   string
		status int
		kind   string
	}{
		{name: "missing network", body: `{"floatingip":{}}`, status: http.StatusBadRequest, kind: "BadRequest"},
		{
			name: "read-only router",
			body: fmt.Sprintf(`{"floatingip":{"floating_network_id":%q,"router_id":%q}}`,
				resources.externalNetworkID, resources.routerID),
			status: http.StatusBadRequest, kind: "BadRequest",
		},
		{
			name: "requested address",
			body: fmt.Sprintf(`{"floatingip":{"floating_network_id":%q,"floating_ip_address":"203.0.113.9"}}`,
				resources.externalNetworkID),
			status: http.StatusForbidden, kind: "PolicyNotAuthorized",
		},
		{
			name: "subnet selection",
			body: fmt.Sprintf(`{"floatingip":{"floating_network_id":%q,"subnet_id":%q}}`,
				resources.externalNetworkID, externalSubnetID),
			status: http.StatusBadRequest, kind: "BadRequest",
		},
		{
			name: "foreign project",
			body: fmt.Sprintf(`{"floatingip":{"floating_network_id":%q,"project_id":%q}}`,
				resources.externalNetworkID, externalProjectID),
			status: http.StatusBadRequest, kind: "BadRequest",
		},
		{
			name: "fixed IP without port",
			body: fmt.Sprintf(`{"floatingip":{"floating_network_id":%q,"fixed_ip_address":%q}}`,
				resources.externalNetworkID, resources.fixedIP),
			status: http.StatusBadRequest, kind: "BadRequest",
		},
		{
			name: "missing port",
			body: fmt.Sprintf(`{"floatingip":{"floating_network_id":%q,"port_id":%q}}`,
				resources.externalNetworkID, missingNetworkMarker),
			status: http.StatusNotFound, kind: "PortNotFound",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, body := send(http.MethodPost, floatingIPsPath, tc.body)

			assert.Equal(t, tc.status, status, "%s", body)
			assertPortNeutronError(t, body, tc.kind)
		})
	}

	// https://docs.openstack.org/api-ref/network/v2/#create-floating-ip
	status, body = send(http.MethodPost, floatingIPsPath, createBody)

	require.Equal(t, http.StatusCreated, status, "%s", body)
	var created struct {
		FloatingIP map[string]json.RawMessage `json:"floatingip"`
	}
	require.NoError(t, json.Unmarshal(body, &created))
	var floatingIPID string
	require.NoError(t, json.Unmarshal(created.FloatingIP["id"], &floatingIPID))
	expected := func(status string) string {
		return fmt.Sprintf(`{"floatingip":{
			"id":%q,"project_id":%q,"tenant_id":%q,"description":"",
			"floating_network_id":%q,"floating_ip_address":"203.0.113.3",
			"router_id":%q,"port_id":%q,"fixed_ip_address":%q,"status":%q,"tags":[]
		}}`, floatingIPID, projectID, projectID, resources.externalNetworkID,
			resources.routerID, resources.portID, resources.fixedIP, status)
	}
	assert.JSONEq(t, expected("DOWN"), string(body))
	status, body = send(http.MethodPost, floatingIPsPath, createBody)
	assert.Equal(t, http.StatusConflict, status, "%s", body)
	assertPortNeutronError(t, body, "FloatingIPPortAlreadyAssociated")

	// https://docs.openstack.org/api-ref/network/v2/#show-floating-ip-details
	path := floatingIPsPath + "/" + floatingIPID
	status, body = send(http.MethodGet, path, "")
	require.Equal(t, http.StatusOK, status, "%s", body)
	assert.JSONEq(t, expected("ACTIVE"), string(body))

	// https://docs.openstack.org/api-ref/network/v2/#list-floating-ips
	idle, idleBody := send(http.MethodPost, floatingIPsPath,
		fmt.Sprintf(`{"floatingip":{"floating_network_id":%q,"description":"spare"}}`, resources.externalNetworkID))
	require.Equal(t, http.StatusCreated, idle, "%s", idleBody)
	var spare struct {
		FloatingIP struct {
			ID string `json:"id"`
		} `json:"floatingip"`
	}
	require.NoError(t, json.Unmarshal(idleBody, &spare))
	for _, tc := range []struct {
		query string
		want  []string
	}{
		{query: "", want: []string{floatingIPID, spare.FloatingIP.ID}},
		{query: "port_id=" + resources.portID, want: []string{floatingIPID}},
		{query: "router_id=" + resources.routerID, want: []string{floatingIPID}},
		{query: "fixed_ip_address=" + resources.fixedIP, want: []string{floatingIPID}},
		{query: "floating_ip_address=203.0.113.4", want: []string{spare.FloatingIP.ID}},
		{query: "status=DOWN", want: []string{spare.FloatingIP.ID}},
		{query: "status=ACTIVE&status=DOWN", want: []string{floatingIPID, spare.FloatingIP.ID}},
		{query: "floating_network_id=" + resources.networkID, want: []string{}},
		{query: "port_id=", want: []string{}},
	} {
		t.Run(tc.query, func(t *testing.T) {
			status, body := send(http.MethodGet, floatingIPsPath+"?"+tc.query, "")

			require.Equal(t, http.StatusOK, status, "%s", body)
			var list struct {
				FloatingIPs []struct {
					ID string `json:"id"`
				} `json:"floatingips"`
			}
			require.NoError(t, json.Unmarshal(body, &list))
			require.NotNil(t, list.FloatingIPs)
			ids := []string{}
			for _, floatingIP := range list.FloatingIPs {
				ids = append(ids, floatingIP.ID)
			}
			assert.ElementsMatch(t, tc.want, ids)
		})
	}
	status, body = send(http.MethodGet, floatingIPsPath+"?description=spare", "")
	assert.Equal(t, http.StatusBadRequest, status, "%s", body)
	status, body = send(http.MethodGet, floatingIPsPath+"?limit=1", "")
	require.Equal(t, http.StatusOK, status, "%s", body)
	var page struct {
		FloatingIPs []json.RawMessage  `json:"floatingips"`
		Links       []gophercloud.Link `json:"floatingips_links"`
	}
	require.NoError(t, json.Unmarshal(body, &page))
	assert.Len(t, page.FloatingIPs, 1)
	require.Len(t, page.Links, 1)
	assert.Equal(t, "next", page.Links[0].Rel)
	next, err := url.Parse(page.Links[0].Href)
	require.NoError(t, err)
	assert.Equal(t, "1", next.Query().Get("limit"))
	status, body = send(http.MethodGet, floatingIPsPath+"?limit=1&marker="+missingNetworkMarker, "")
	assert.Equal(t, http.StatusNotFound, status, "%s", body)
	assertPortNeutronError(t, body, "FloatingIPNotFound")

	// https://docs.openstack.org/api-ref/network/v2/#update-floating-ip
	for _, tc := range []struct {
		name string
		body string
	}{
		{name: "read-only network", body: fmt.Sprintf(`{"floatingip":{"floating_network_id":%q}}`, resources.networkID)},
		{name: "fixed IP without port", body: fmt.Sprintf(`{"floatingip":{"fixed_ip_address":%q}}`, resources.fixedIP)},
		{name: "IPv6 fixed IP", body: fmt.Sprintf(`{"floatingip":{"port_id":%q,"fixed_ip_address":"2001:db8::1"}}`, resources.portID)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, body := send(http.MethodPut, path, tc.body)

			assert.Equal(t, http.StatusBadRequest, status, "%s", body)
			assertPortNeutronError(t, body, "BadRequest")
		})
	}
	status, body = send(http.MethodPut, path, `{"floatingip":{"port_id":null,"description":"idle"}}`)

	require.Equal(t, http.StatusOK, status, "%s", body)
	assert.JSONEq(t, fmt.Sprintf(`{"floatingip":{
		"id":%q,"project_id":%q,"tenant_id":%q,"description":"idle",
		"floating_network_id":%q,"floating_ip_address":"203.0.113.3",
		"router_id":null,"port_id":null,"fixed_ip_address":null,"status":"ACTIVE","tags":[]
	}}`, floatingIPID, projectID, projectID, resources.externalNetworkID), string(body))
	status, body = send(http.MethodGet, path, "")
	require.Equal(t, http.StatusOK, status, "%s", body)
	assert.Contains(t, string(body), `"status":"DOWN"`)

	// https://docs.openstack.org/api-ref/network/v2/#delete-floating-ip
	status, body = send(http.MethodDelete, path, `{"floatingip":{}}`)
	assert.Equal(t, http.StatusBadRequest, status, "%s", body)
	status, _, body = request(t, instance, http.MethodDelete, path, "", auth)
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	status, body = send(http.MethodGet, path, "")
	assert.Equal(t, http.StatusNotFound, status, "%s", body)
	assertPortNeutronError(t, body, "FloatingIPNotFound")

	status, _, body = request(t, instance, http.MethodPost, "/_sandstack/reset", "", admin)
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	auth["X-Auth-Token"] = authenticate(t, instance).Token()
	status, body = send(http.MethodGet, floatingIPsPath, "")
	require.Equal(t, http.StatusOK, status, "%s", body)
	assert.JSONEq(t, `{"floatingips":[]}`, string(body))
}
