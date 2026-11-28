package server_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/portsecurity"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/networks"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/ports"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type securedPort struct {
	ports.Port
	portsecurity.PortSecurityExt
}

type securedNetwork struct {
	networks.Network
	portsecurity.PortSecurityExt
}

func TestGophercloudManagesNeutronPortSecurityAndAddressPairs(t *testing.T) {
	instance := startServer(t, "")
	resources := createRouterInterfaceResources(t, authenticate(t, instance))
	client := resources.service
	ctx := t.Context()
	enabled, disabled := true, false
	assertNeutronFailure := func(err error, status int, kind string) {
		t.Helper()
		var failure gophercloud.ErrUnexpectedResponseCode
		require.ErrorAs(t, err, &failure)
		assert.Equal(t, status, failure.Actual, "%s", failure.Body)
		assertPortNeutronError(t, failure.Body, kind)
	}

	// https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/plugins/ml2/extensions/port_security.py#L38-L81
	var insecureNetwork securedNetwork
	err := networks.Create(ctx, client, portsecurity.NetworkCreateOptsExt{
		CreateOptsBuilder:   networks.CreateOpts{Name: "insecure"},
		PortSecurityEnabled: &disabled,
	}).ExtractInto(&insecureNetwork)
	require.NoError(t, err)
	var inherited securedPort
	err = ports.Create(ctx, client, ports.CreateOpts{
		NetworkID: insecureNetwork.ID,
	}).ExtractInto(&inherited)
	require.NoError(t, err)
	var overridden securedPort
	err = ports.Create(ctx, client, portsecurity.PortCreateOptsExt{
		CreateOptsBuilder:   ports.CreateOpts{NetworkID: insecureNetwork.ID},
		PortSecurityEnabled: &enabled,
	}).ExtractInto(&overridden)
	require.NoError(t, err)

	assert.False(t, insecureNetwork.PortSecurityEnabled)
	assert.False(t, inherited.PortSecurityEnabled)
	assert.Equal(t, []string{}, inherited.SecurityGroups)
	assert.Equal(t, []ports.AddressPair{}, inherited.AllowedAddressPairs)
	assert.True(t, overridden.PortSecurityEnabled)
	var reenabled securedNetwork
	err = networks.Update(ctx, client, insecureNetwork.ID, portsecurity.NetworkUpdateOptsExt{
		UpdateOptsBuilder:   networks.UpdateOpts{},
		PortSecurityEnabled: &enabled,
	}).ExtractInto(&reenabled)
	require.NoError(t, err)
	assert.True(t, reenabled.PortSecurityEnabled)

	// https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/db/allowedaddresspairs_db.py#L90-L128
	var secured securedPort
	err = ports.Create(ctx, client, ports.CreateOpts{
		NetworkID: resources.networkID,
		AllowedAddressPairs: []ports.AddressPair{
			{IPAddress: "10.66.0.200"},
			{IPAddress: "192.0.2.0/24", MACAddress: "fa:16:3e:aa:bb:cc"},
		},
	}).ExtractInto(&secured)
	require.NoError(t, err)
	require.Len(t, secured.SecurityGroups, 1)
	assert.True(t, secured.PortSecurityEnabled)
	assert.Equal(t, []ports.AddressPair{
		{IPAddress: "10.66.0.200", MACAddress: secured.MACAddress},
		{IPAddress: "192.0.2.0/24", MACAddress: "fa:16:3e:aa:bb:cc"},
	}, secured.AllowedAddressPairs)

	// https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/plugins/ml2/plugin.py#L1900-L1936
	disable := func(opts ports.UpdateOpts) error {
		return ports.Update(ctx, client, secured.ID, portsecurity.PortUpdateOptsExt{
			UpdateOptsBuilder:   opts,
			PortSecurityEnabled: &disabled,
		}).Err
	}
	noPairs := []ports.AddressPair{}
	assertNeutronFailure(
		disable(ports.UpdateOpts{}),
		http.StatusConflict, "AddressPairAndPortSecurityRequired",
	)
	assertNeutronFailure(
		disable(ports.UpdateOpts{AllowedAddressPairs: &noPairs}),
		http.StatusConflict, "PortSecurityPortHasSecurityGroup",
	)
	assertNeutronFailure(
		disable(ports.UpdateOpts{AllowedAddressPairs: &noPairs, SecurityGroups: &secured.SecurityGroups}),
		http.StatusBadRequest, "PortSecurityAndIPRequiredForSecurityGroups",
	)
	var unchanged securedPort
	require.NoError(t, ports.Get(ctx, client, secured.ID).ExtractInto(&unchanged))
	assert.Equal(t, secured, unchanged)

	var insecure securedPort
	err = ports.Update(ctx, client, secured.ID, portsecurity.PortUpdateOptsExt{
		UpdateOptsBuilder: ports.UpdateOpts{
			AllowedAddressPairs: &noPairs, SecurityGroups: &[]string{},
		},
		PortSecurityEnabled: &disabled,
	}).ExtractInto(&insecure)
	require.NoError(t, err)
	assert.False(t, insecure.PortSecurityEnabled)
	assert.Equal(t, []string{}, insecure.SecurityGroups)
	assert.Equal(t, []ports.AddressPair{}, insecure.AllowedAddressPairs)
	_, err = ports.Update(ctx, client, secured.ID, ports.UpdateOpts{
		AllowedAddressPairs: &[]ports.AddressPair{{IPAddress: "10.66.0.201"}},
	}).Extract()
	assertNeutronFailure(err, http.StatusConflict, "AddressPairAndPortSecurityRequired")

	pages, err := ports.List(client, ports.ListOpts{ID: secured.ID}).AllPages(ctx)
	require.NoError(t, err)
	var listed []securedPort
	require.NoError(t, ports.ExtractPortsInto(pages, &listed))
	assert.Equal(t, []securedPort{insecure}, listed)
}

func TestNeutronPortSecurityWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	resources := createRouterInterfaceResources(t, provider)
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	send := func(method, path, body string) (int, []byte) {
		t.Helper()
		status, headers, response := request(t, instance, method, path, body, auth)
		requireJSON(t, headers)
		return status, response
	}
	type wireSecuredPort struct {
		ID                  string            `json:"id"`
		MACAddress          string            `json:"mac_address"`
		SecurityGroups      []string          `json:"security_groups"`
		PortSecurityEnabled *bool             `json:"port_security_enabled"`
		AllowedAddressPairs []json.RawMessage `json:"allowed_address_pairs"`
	}
	decodePort := func(body []byte) wireSecuredPort {
		t.Helper()
		var response struct {
			Port wireSecuredPort `json:"port"`
		}
		require.NoError(t, json.Unmarshal(body, &response))
		require.NotNil(t, response.Port.PortSecurityEnabled)
		require.NotNil(t, response.Port.AllowedAddressPairs)
		return response.Port
	}

	// https://github.com/openstack/neutron-lib/blob/5.0.0/neutron_lib/api/definitions/port_security.py#L59-L74
	status, body := send(http.MethodPost, networksPath, `{"network":{"name":"wire"}}`)
	require.Equal(t, http.StatusCreated, status, "%s", body)
	assert.Contains(t, string(body), `"port_security_enabled":true`)
	status, body = send(http.MethodPost, portsPath, fmt.Sprintf(
		`{"port":{"network_id":%q,"allowed_address_pairs":null}}`, resources.networkID,
	))
	require.Equal(t, http.StatusCreated, status, "%s", body)
	plain := decodePort(body)
	assert.True(t, *plain.PortSecurityEnabled)
	assert.Empty(t, plain.AllowedAddressPairs)
	status, body = send(http.MethodPost, portsPath, fmt.Sprintf(
		`{"port":{"network_id":%q,"allowed_address_pairs":[
			{"ip_address":"10.66.0.2"},
			{"ip_address":"2001:db8::/64","mac_address":"FA-16-3E-00-00-01"}
		]}}`, resources.networkID,
	))
	require.Equal(t, http.StatusCreated, status, "%s", body)
	paired := decodePort(body)
	require.Len(t, paired.AllowedAddressPairs, 2)
	assert.JSONEq(t,
		fmt.Sprintf(`{"ip_address":"10.66.0.2","mac_address":%q}`, paired.MACAddress),
		string(paired.AllowedAddressPairs[0]),
	)
	assert.JSONEq(t,
		`{"ip_address":"2001:db8::/64","mac_address":"FA-16-3E-00-00-01"}`,
		string(paired.AllowedAddressPairs[1]),
	)
	status, body = send(http.MethodGet, portsPath+"/"+paired.ID, "")
	require.Equal(t, http.StatusOK, status, "%s", body)
	assert.Equal(t, paired, decodePort(body))
	status, body = send(http.MethodPut, portsPath+"/"+paired.ID, `{"port":{"allowed_address_pairs":null}}`)
	require.Equal(t, http.StatusOK, status, "%s", body)
	assert.Empty(t, decodePort(body).AllowedAddressPairs)

	// https://github.com/openstack/neutron-lib/blob/5.0.0/neutron_lib/api/validators/allowedaddresspairs.py#L23-L92
	// https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/conf/extensions/allowedaddresspairs.py#L19-L29
	tooMany := make([]string, 11)
	for i := range tooMany {
		tooMany[i] = fmt.Sprintf(`{"ip_address":"192.0.2.%d"}`, i+1)
	}
	tenPairs := strings.Join(tooMany[:10], ",")
	group := plain.SecurityGroups[0]
	for _, tc := range []struct {
		name   string
		fields string
		status int
		kind   string
	}{
		{"not a list", `"allowed_address_pairs":{"ip_address":"192.0.2.1"}`, 400, "HTTPBadRequest"},
		{"not an object", `"allowed_address_pairs":["ip_address"]`, 400, "HTTPBadRequest"},
		{"invalid address", `"allowed_address_pairs":[{"ip_address":"10.0.0.1222"}]`, 400, "HTTPBadRequest"},
		{"leading zero", `"allowed_address_pairs":[{"ip_address":"10.0.0.01"}]`, 400, "HTTPBadRequest"},
		{"invalid prefix", `"allowed_address_pairs":[{"ip_address":"10.0.0.0/33"}]`, 400, "HTTPBadRequest"},
		{"multicast address", `"allowed_address_pairs":[{"ip_address":"224.0.0.1"}]`, 400, "HTTPBadRequest"},
		{"multicast prefix", `"allowed_address_pairs":[{"ip_address":"192.0.0.0/2"}]`, 400, "HTTPBadRequest"},
		{"invalid mac", `"allowed_address_pairs":[{"ip_address":"192.0.2.1","mac_address":"invalid_mac"}]`,
			400, "HTTPBadRequest"},
		{"zero mac", `"allowed_address_pairs":[{"ip_address":"192.0.2.1","mac_address":"00:00:00:00:00:00"}]`,
			400, "HTTPBadRequest"},
		{"multicast mac", `"allowed_address_pairs":[{"ip_address":"192.0.2.1","mac_address":"01:00:5e:00:00:01"}]`,
			400, "HTTPBadRequest"},
		{"extra attribute", `"allowed_address_pairs":[{"ip_address":"192.0.2.1","icbb":"agreed"}]`,
			400, "HTTPBadRequest"},
		{"missing address", `"allowed_address_pairs":[{"mac_address":"fa:16:3e:00:00:01"}]`,
			400, "AllowedAddressPairsMissingIP"},
		{"duplicate pair", `"allowed_address_pairs":[{"ip_address":"192.0.2.1"},{"ip_address":"192.0.2.1"}]`,
			400, "DuplicateAddressPairInRequest"},
		{"too many pairs", `"allowed_address_pairs":[` + strings.Join(tooMany, ",") + `]`,
			400, "AllowedAddressPairExhausted"},
		{"non-boolean port security", `"port_security_enabled":"false"`, 400, "BadRequest"},
		{"pairs without port security",
			`"port_security_enabled":false,"allowed_address_pairs":[{"ip_address":"192.0.2.1"}]`,
			409, "AddressPairAndPortSecurityRequired"},
		{"groups without port security",
			fmt.Sprintf(`"port_security_enabled":false,"security_groups":[%q]`, group),
			400, "PortSecurityAndIPRequiredForSecurityGroups"},
	} {
		t.Run("create "+tc.name, func(t *testing.T) {
			status, body := send(http.MethodPost, portsPath, fmt.Sprintf(
				`{"port":{"network_id":%q,%s}}`, resources.networkID, tc.fields,
			))

			assert.Equal(t, tc.status, status, "%s", body)
			assertPortNeutronError(t, body, tc.kind)
		})
	}
	status, body = send(http.MethodPost, portsPath, fmt.Sprintf(
		`{"port":{"network_id":%q,"allowed_address_pairs":[%s]}}`, resources.networkID, tenPairs,
	))
	require.Equal(t, http.StatusCreated, status, "%s", body)
	assert.Len(t, decodePort(body).AllowedAddressPairs, 10)
	status, body = send(http.MethodPost, portsPath, fmt.Sprintf(
		`{"port":{"network_id":%q,"port_security_enabled":false,"security_groups":[],
		"allowed_address_pairs":[]}}`, resources.networkID,
	))
	require.Equal(t, http.StatusCreated, status, "%s", body)
	assert.False(t, *decodePort(body).PortSecurityEnabled)

	// https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/tests/unit/db/test_allowedaddresspairs_db.py#L396-L409
	path := portsPath + "/" + plain.ID
	for _, tc := range []struct {
		name   string
		fields string
		status int
		kind   string
	}{
		{"own mac duplicate", fmt.Sprintf(
			`"allowed_address_pairs":[{"ip_address":"10.0.0.1"},{"ip_address":"10.0.0.1","mac_address":%q}]`,
			plain.MACAddress,
		), 400, "DuplicateAddressPairInRequest"},
		{"invalid address", `"allowed_address_pairs":[{"ip_address":"not-an-ip"}]`, 400, "HTTPBadRequest"},
		{"disable with groups", `"port_security_enabled":false`, 409, "PortSecurityPortHasSecurityGroup"},
	} {
		t.Run("update "+tc.name, func(t *testing.T) {
			status, body := send(http.MethodPut, path, fmt.Sprintf(`{"port":{%s}}`, tc.fields))

			assert.Equal(t, tc.status, status, "%s", body)
			assertPortNeutronError(t, body, tc.kind)
		})
	}
	status, body = send(http.MethodGet, path, "")
	require.Equal(t, http.StatusOK, status, "%s", body)
	assert.Equal(t, plain, decodePort(body))
	status, body = send(http.MethodPut, path, fmt.Sprintf(
		`{"port":{"allowed_address_pairs":[{"ip_address":%q}]}}`, "10.66.0.2",
	))
	require.Equal(t, http.StatusOK, status, "%s", body)
	assert.Len(t, decodePort(body).AllowedAddressPairs, 1)
}
