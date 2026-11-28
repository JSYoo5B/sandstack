package server_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/instanceactions"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/servers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	fixtureConsoleOutput = "[    0.000000] Linux version 6.8.0-sandstack (fixture@sandstack) #1 SMP\n" +
		"[    1.000000] Run /sbin/init as init process\n" +
		"cloud-init: modules:final finished\n" +
		"fixture-server login: "
	consoleControlPath = "/_sandstack/compute/servers/" + fixtureServerID + "/console-output"
)

func TestGophercloudReadsNovaConsoleOutput(t *testing.T) {
	instance := startServer(t, "")
	newCompute := func() *gophercloud.ServiceClient {
		t.Helper()
		client, err := openstack.NewComputeV2(authenticate(t, instance), gophercloud.EndpointOpts{
			Region: "RegionOne", Availability: gophercloud.AvailabilityPublic,
		})
		require.NoError(t, err)
		return client
	}
	compute := newCompute()
	ctx := t.Context()
	admin := map[string]string{"X-SandStack-Admin-Token": adminToken}
	read := func(length int) string {
		t.Helper()
		output, err := servers.ShowConsoleOutput(ctx, compute, fixtureServerID, servers.ShowConsoleOutputOpts{
			Length: length,
		}).Extract()
		require.NoError(t, err)
		return output
	}

	full := read(0)
	tail := read(2)

	assert.Equal(t, fixtureConsoleOutput, full)
	assert.Equal(t, "cloud-init: modules:final finished\nfixture-server login: ", tail)

	status, _, body := request(t, instance, http.MethodPut, consoleControlPath,
		"boot\nready\nlogin: ", admin)
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	require.NoError(t, servers.Stop(ctx, compute, fixtureServerID).ExtractErr())
	require.NoError(t, servers.Lock(ctx, compute, fixtureServerID).ExtractErr())

	replaced := read(0)
	replacedTail := read(1)
	pages, err := instanceactions.List(compute, fixtureServerID, nil).AllPages(ctx)
	require.NoError(t, err)
	actions, err := instanceactions.ExtractInstanceActions(pages)
	require.NoError(t, err)

	assert.Equal(t, "boot\nready\nlogin: ", replaced)
	assert.Equal(t, "login: ", replacedTail)
	require.Len(t, actions, 3)
	assert.Equal(t, "lock", actions[0].Action)

	status, _, body = request(t, instance, http.MethodDelete, consoleControlPath, "", admin)
	require.Equal(t, http.StatusNoContent, status, "%s", body)

	assert.Equal(t, fixtureConsoleOutput, read(0))

	status, _, body = request(t, instance, http.MethodPut, consoleControlPath, "changed", admin)
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	status, _, body = request(t, instance, http.MethodPost, "/_sandstack/reset", "", admin)
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	compute = newCompute()

	assert.Equal(t, fixtureConsoleOutput, read(0))
	_, err = servers.ShowConsoleOutput(ctx, compute, "00000000-0000-0000-0000-000000000000",
		servers.ShowConsoleOutputOpts{}).Extract()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusNotFound), "%v", err)
}

func TestNovaConsoleOutputWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	projectPath := computePath + "/" + routerProjectID(t, provider)
	action := projectPath + "/servers/" + fixtureServerID + "/action"
	auth := map[string]string{"X-Auth-Token": provider.Token(), "OpenStack-API-Version": "compute 2.1"}
	admin := map[string]string{"X-SandStack-Admin-Token": adminToken}
	status, _, body := request(t, instance, http.MethodPut, consoleControlPath, "0\n1\n2\n3\n4\n", admin)
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	output := func(requestBody string) string {
		t.Helper()
		status, headers, body := request(t, instance, http.MethodPost, action, requestBody, auth)
		require.Equal(t, http.StatusOK, status, "%s: %s", requestBody, body)
		requireJSON(t, headers)
		var fields map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(body, &fields))
		require.Len(t, fields, 1)
		var text string
		require.NoError(t, json.Unmarshal(fields["output"], &text))
		return text
	}

	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/schemas/console_output.py#L15-L36
	for _, requestBody := range []string{
		`{"os-getConsoleOutput":null}`,
		`{"os-getConsoleOutput":[]}`,
		`{"os-getConsoleOutput":{"length":-2}}`,
		`{"os-getConsoleOutput":{"length":"-2"}}`,
		`{"os-getConsoleOutput":{"length":"NaN"}}`,
		`{"os-getConsoleOutput":{"length":" 2"}}`,
		`{"os-getConsoleOutput":{"length":2.5}}`,
		`{"os-getConsoleOutput":{"length":true}}`,
		`{"os-getConsoleOutput":{"length":1,"other":1}}`,
		`{"os-getConsoleOutput":{},"pause":null}`,
	} {
		status, headers, body := request(t, instance, http.MethodPost, action, requestBody, auth)

		requireNovaFault(t, http.StatusBadRequest, "badRequest", status, headers, body)
	}

	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/manager.py#L7561-L7584
	assert.Equal(t, "0\n1\n2\n3\n4\n", output(`{"os-getConsoleOutput":{}}`))
	assert.Equal(t, "0\n1\n2\n3\n4\n", output(`{"os-getConsoleOutput":{"length":null}}`))
	assert.Equal(t, "3\n4\n", output(`{"os-getConsoleOutput":{"length":3}}`))
	assert.Equal(t, "3\n4\n", output(`{"os-getConsoleOutput":{"length":"03"}}`))
	assert.Equal(t, "3\n4\n", output(`{"os-getConsoleOutput":{"length":3.0}}`))
	assert.Equal(t, "", output(`{"os-getConsoleOutput":{"length":0}}`))
	assert.Equal(t, "1\n2\n3\n4\n", output(`{"os-getConsoleOutput":{"length":-1}}`))
	assert.Equal(t, "1\n2\n3\n4\n", output(`{"os-getConsoleOutput":{"length":"-1"}}`))
	assert.Equal(t, "0\n1\n2\n3\n4\n", output(`{"os-getConsoleOutput":{"length":"99999999999999999999"}}`))

	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/console_output.py#L67-L73
	status, _, body = request(t, instance, http.MethodPut, consoleControlPath, "a\x01b\r\nc\td\x7f\xc3\xa9", admin)
	require.Equal(t, http.StatusNoContent, status, "%s", body)

	assert.Equal(t, "ab\nc\td\x7f��", output(`{"os-getConsoleOutput":{}}`))

	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/console_output.py#L40-L45
	const missing = "/servers/00000000-0000-0000-0000-000000000000/action"
	status, headers, body := request(t, instance, http.MethodPost, projectPath+missing,
		`{"os-getConsoleOutput":{}}`, auth)

	requireNovaFault(t, http.StatusNotFound, "itemNotFound", status, headers, body)

	status, _, _ = request(t, instance, http.MethodPut, consoleControlPath, "unauthorized", nil)
	assert.Equal(t, http.StatusUnauthorized, status)
	status, _, _ = request(t, instance, http.MethodPut,
		"/_sandstack/compute/servers/00000000-0000-0000-0000-000000000000/console-output", "x", admin)
	assert.Equal(t, http.StatusNotFound, status)
}
