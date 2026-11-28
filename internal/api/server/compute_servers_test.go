package server_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/volumes"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/keypairs"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/servers"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/volumeattach"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/layer3/floatingips"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/security/groups"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/networks"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/ports"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/subnets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const serverCreateBody = `{"server":{"name":"web","imageRef":"` + fixtureImageID + `","flavorRef":"1"}}`

type bootClients struct {
	compute *gophercloud.ServiceClient
	network *gophercloud.ServiceClient
	volume  *gophercloud.ServiceClient
}

func newBootClients(t *testing.T, instance *httptest.Server) bootClients {
	t.Helper()
	provider := authenticate(t, instance)
	opts := gophercloud.EndpointOpts{Region: "RegionOne", Availability: gophercloud.AvailabilityPublic}
	compute, err := openstack.NewComputeV2(provider, opts)
	require.NoError(t, err)
	network, err := openstack.NewNetworkV2(provider, opts)
	require.NoError(t, err)
	volume, err := openstack.NewBlockStorageV3(provider, opts)
	require.NoError(t, err)
	return bootClients{compute: compute, network: network, volume: volume}
}

func waitForServer(t *testing.T, client *gophercloud.ServiceClient, id, status string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	require.NoError(t, servers.WaitForStatus(ctx, client, id, status))
}

func serverPorts(t *testing.T, client *gophercloud.ServiceClient, serverID string) []ports.Port {
	t.Helper()
	pages, err := ports.List(client, ports.ListOpts{DeviceID: serverID}).AllPages(t.Context())
	require.NoError(t, err)
	found, err := ports.ExtractPorts(pages)
	require.NoError(t, err)
	return found
}

// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/servers.py#L700-L923
func TestGophercloudBootsAndDeletesNovaServer(t *testing.T) {
	instance := startServer(t, "")
	clients := newBootClients(t, instance)
	ctx := t.Context()
	subnet, err := subnets.Create(ctx, clients.network, subnets.CreateOpts{
		NetworkID: fixtureNetworkID, CIDR: "10.20.0.0/24", IPVersion: gophercloud.IPv4,
	}).Extract()
	require.NoError(t, err)
	web, err := groups.Create(ctx, clients.network, groups.CreateOpts{Name: "web"}).Extract()
	require.NoError(t, err)
	_, err = keypairs.Create(ctx, clients.compute, keypairs.CreateOpts{Name: "deploy"}).Extract()
	require.NoError(t, err)

	created, err := servers.Create(ctx, clients.compute, keypairs.CreateOptsExt{
		CreateOptsBuilder: servers.CreateOpts{
			Name:           "web-1",
			ImageRef:       fixtureImageID,
			FlavorRef:      "1",
			SecurityGroups: []string{"web"},
			Metadata:       map[string]string{"role": "web"},
		},
		KeyName: "deploy",
	}, nil).Extract()

	require.NoError(t, err)
	require.NotEmpty(t, created.ID)
	assert.Len(t, created.AdminPass, 12)
	assert.Equal(t, []map[string]any{{"name": "web"}}, created.SecurityGroups)
	waitForServer(t, clients.compute, created.ID, "ACTIVE")
	server, err := servers.Get(ctx, clients.compute, created.ID).Extract()
	require.NoError(t, err)
	assert.Equal(t, "web-1", server.Name)
	assert.Equal(t, "deploy", server.KeyName)
	assert.Equal(t, map[string]string{"role": "web"}, server.Metadata)
	assert.Equal(t, fixtureImageID, server.Image["id"])
	assert.Equal(t, "1", server.Flavor["id"])
	assert.Equal(t, []map[string]any{{"name": "web"}}, server.SecurityGroups)
	attached := serverPorts(t, clients.network, created.ID)
	require.Len(t, attached, 1)
	assert.Equal(t, "compute:nova", attached[0].DeviceOwner)
	assert.Equal(t, []string{web.ID}, attached[0].SecurityGroups)
	require.Len(t, attached[0].FixedIPs, 1)
	assert.Equal(t, subnet.ID, attached[0].FixedIPs[0].SubnetID)
	assert.Equal(t, map[string]any{"fixture-network": []any{map[string]any{
		"version":                 float64(4),
		"addr":                    attached[0].FixedIPs[0].IPAddress,
		"OS-EXT-IPS:type":         "fixed",
		"OS-EXT-IPS-MAC:mac_addr": attached[0].MACAddress,
	}}}, server.Addresses)
	listed := listComputeServers(t, clients.compute, "")
	require.Len(t, listed, 2)
	assert.Equal(t, created.ID, listed[0].ID)
	assert.Equal(t, fixtureServerID, listed[1].ID)
	_, err = volumeattach.Create(ctx, clients.compute, created.ID, volumeattach.CreateOpts{
		VolumeID: fixtureVolumeID,
	}).Extract()
	require.NoError(t, err)

	err = servers.Delete(ctx, clients.compute, created.ID).ExtractErr()

	require.NoError(t, err)
	_, err = servers.Get(ctx, clients.compute, created.ID).Extract()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusNotFound), "%v", err)
	assert.Empty(t, serverPorts(t, clients.network, created.ID))
	_, err = ports.Get(ctx, clients.network, attached[0].ID).Extract()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusNotFound), "%v", err)
	volume, err := volumes.Get(ctx, clients.volume, fixtureVolumeID).Extract()
	require.NoError(t, err)
	assert.Equal(t, "available", volume.Status)
	remaining := listComputeServers(t, clients.compute, "")
	require.Len(t, remaining, 1)
	assert.Equal(t, fixtureServerID, remaining[0].ID)
}

// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/network/neutron.py#L1841-L1866
func TestGophercloudBootsServersOnPortsWithFloatingIPs(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	resources := createFloatingIPResources(t, provider)
	clients := newBootClients(t, instance)
	ctx := t.Context()
	onPort, err := servers.Create(ctx, clients.compute, servers.CreateOpts{
		Name: "on-port", ImageRef: fixtureImageID, FlavorRef: "1",
		Networks: []servers.Network{{Port: resources.portID}},
	}, nil).Extract()
	require.NoError(t, err)
	onNetwork, err := servers.Create(ctx, clients.compute, servers.CreateOpts{
		Name: "on-network", ImageRef: fixtureImageID, FlavorRef: "1",
		Networks: []servers.Network{{UUID: resources.networkID}},
	}, nil).Extract()
	require.NoError(t, err)
	waitForServer(t, clients.compute, onPort.ID, "ACTIVE")
	waitForServer(t, clients.compute, onNetwork.ID, "ACTIVE")
	createdPorts := serverPorts(t, clients.network, onNetwork.ID)
	require.Len(t, createdPorts, 1)
	kept, err := floatingips.Create(ctx, clients.network, floatingips.CreateOpts{
		FloatingNetworkID: resources.externalNetworkID, PortID: resources.portID,
	}).Extract()
	require.NoError(t, err)
	released, err := floatingips.Create(ctx, clients.network, floatingips.CreateOpts{
		FloatingNetworkID: resources.externalNetworkID, PortID: createdPorts[0].ID,
	}).Extract()
	require.NoError(t, err)

	server, err := servers.Get(ctx, clients.compute, onPort.ID).Extract()

	require.NoError(t, err)
	bound, err := ports.Get(ctx, clients.network, resources.portID).Extract()
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"router-network": []any{
		map[string]any{
			"version": float64(4), "addr": resources.fixedIP,
			"OS-EXT-IPS:type": "fixed", "OS-EXT-IPS-MAC:mac_addr": bound.MACAddress,
		},
		map[string]any{
			"version": float64(4), "addr": kept.FloatingIP,
			"OS-EXT-IPS:type": "floating", "OS-EXT-IPS-MAC:mac_addr": bound.MACAddress,
		},
	}}, server.Addresses)
	assert.Equal(t, onPort.ID, bound.DeviceID)
	assert.Equal(t, "compute:nova", bound.DeviceOwner)
	_, err = servers.Create(ctx, clients.compute, servers.CreateOpts{
		Name: "port-in-use", ImageRef: fixtureImageID, FlavorRef: "1",
		Networks: []servers.Network{{Port: resources.portID}},
	}, nil).Extract()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusConflict), "%v", err)

	require.NoError(t, servers.Delete(ctx, clients.compute, onPort.ID).ExtractErr())
	require.NoError(t, servers.Delete(ctx, clients.compute, onNetwork.ID).ExtractErr())

	unbound, err := ports.Get(ctx, clients.network, resources.portID).Extract()
	require.NoError(t, err)
	assert.Empty(t, unbound.DeviceID)
	assert.Empty(t, unbound.DeviceOwner)
	stillAssociated, err := floatingips.Get(ctx, clients.network, kept.ID).Extract()
	require.NoError(t, err)
	assert.Equal(t, resources.portID, stillAssociated.PortID)
	disassociated, err := floatingips.Get(ctx, clients.network, released.ID).Extract()
	require.NoError(t, err)
	assert.Empty(t, disassociated.PortID)
	assert.Equal(t, "DOWN", disassociated.Status)
}

type wireCreatedServer struct {
	Server map[string]json.RawMessage `json:"server"`
}

func TestNovaServerCreateAndDeleteWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	clients := newBootClients(t, instance)
	projectID := routerProjectID(t, provider)
	projectPath := computePath + "/" + projectID
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	admin := map[string]string{"X-SandStack-Admin-Token": adminToken}
	create := func(body string) (int, http.Header, []byte) {
		t.Helper()
		return request(t, instance, http.MethodPost, projectPath+"/servers", body, auth)
	}
	withServer := func(fields string) string {
		return `{"server":{"name":"web","imageRef":"` + fixtureImageID + `","flavorRef":"1"` + fields + `}}`
	}
	const missing = "00000000-0000-4000-8000-000000000000"

	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/schemas/servers.py#L154-L230
	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/servers.py#L417-L478
	for _, body := range []string{
		`{"server":{"imageRef":"` + fixtureImageID + `","flavorRef":"1"}}`,
		`{"server":{"name":" web","imageRef":"` + fixtureImageID + `","flavorRef":"1"}}`,
		`{"server":{"name":"web","imageRef":"` + fixtureImageID + `"}}`,
		`{"server":{"name":"web","imageRef":"` + fixtureImageID + `","flavorRef":""}}`,
		`{"server":{"name":"web","flavorRef":"1"}}`,
		`{"server":{"name":"web","imageRef":"","flavorRef":"1"}}`,
		`{"server":{"name":"web","imageRef":"fixture-image","flavorRef":"1"}}`,
		`{"server":{"name":"web","imageRef":"` + fixtureImageID + `","flavorRef":"1"},"extra":{}}`,
		withServer(`,"description":"added in 2.19"`),
		withServer(`,"metadata":{"bad/key":"value"}`),
		withServer(`,"networks":[{"uuid":"fixture-network"}]`),
		withServer(`,"networks":[{"fixed_ip":"10.0.0.5"}]`),
		withServer(`,"networks":[{"uuid":"` + fixtureNetworkID + `","tag":"added in 2.32"}]`),
		withServer(`,"networks":[{"port":"` + missing + `"},{"port":"` + missing + `"}]`),
		withServer(`,"networks":[{"port":"` + missing + `","fixed_ip":"10.0.0.5"}]`),
		withServer(`,"security_groups":[{"name":"web","id":"x"}]`),
		withServer(`,"min_count":2`),
		withServer(`,"block_device_mapping":[]`),
	} {
		status, headers, response := create(body)
		requireNovaFault(t, http.StatusBadRequest, "badRequest", status, headers, response)
	}

	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/servers.py#L835-L910
	for _, test := range []struct {
		body    string
		message string
	}{
		{`{"server":{"name":"web","imageRef":"` + fixtureImageID + `","flavorRef":"42"}}`,
			"Flavor 42 could not be found."},
		{`{"server":{"name":"web","imageRef":"` + missing + `","flavorRef":1}}`, "Can not find requested image"},
		{withServer(`,"security_groups":[{"name":"web"}]`), "Security group web not found."},
		{withServer(`,"networks":[{"uuid":"` + missing + `"}]`), "Network " + missing + " could not be found."},
		{withServer(`,"networks":[{"uuid":"` + fixtureNetworkID + `"}]`),
			"Network " + fixtureNetworkID + " requires a subnet in order to boot instances on."},
		{withServer(`,"networks":[{"port":"` + missing + `"}]`), "Port id " + missing + " could not be found."},
		{withServer(`,"key_name":"missing"`), "Invalid key_name provided."},
	} {
		status, headers, response := create(test.body)
		requireNovaFault(t, http.StatusBadRequest, "badRequest", status, headers, response)
		assert.Contains(t, string(response), test.message)
	}
	metadata := make([]string, 129)
	for i := range metadata {
		metadata[i] = `"key-` + strconv.Itoa(i) + `":"value"`
	}
	status, headers, body := create(withServer(`,"metadata":{` + strings.Join(metadata, ",") + `}`))
	requireNovaFault(t, http.StatusForbidden, "forbidden", status, headers, body)

	ctx := t.Context()
	_, err := subnets.Create(ctx, clients.network, subnets.CreateOpts{
		NetworkID: fixtureNetworkID, CIDR: "10.20.0.0/24", IPVersion: gophercloud.IPv4,
	}).Extract()
	require.NoError(t, err)
	status, _, body = request(t, instance, http.MethodPut,
		"/_sandstack/image/images/"+fixtureImageID+"/deactivated", "", admin)
	require.Equal(t, http.StatusNoContent, status, "%s", body)

	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L745-L746
	status, headers, body = create(serverCreateBody)

	requireNovaFault(t, http.StatusBadRequest, "badRequest", status, headers, body)
	assert.Contains(t, string(body), "Image "+fixtureImageID+" is not active.")
	status, _, body = request(t, instance, http.MethodDelete,
		"/_sandstack/image/images/"+fixtureImageID+"/deactivated", "", admin)
	require.Equal(t, http.StatusNoContent, status, "%s", body)

	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/views/servers.py#L78-L99
	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/servers.py#L103-L114
	status, headers, body = create(serverCreateBody)

	require.Equal(t, http.StatusAccepted, status, "%s", body)
	requireJSON(t, headers)
	var created wireCreatedServer
	require.NoError(t, json.Unmarshal(body, &created))
	assert.ElementsMatch(t, []string{"id", "links", "OS-DCF:diskConfig", "security_groups", "adminPass"},
		keysOf(created.Server))
	var serverID, adminPass string
	require.NoError(t, json.Unmarshal(created.Server["id"], &serverID))
	require.NoError(t, json.Unmarshal(created.Server["adminPass"], &adminPass))
	assert.Len(t, adminPass, 12)
	assert.JSONEq(t, `"MANUAL"`, string(created.Server["OS-DCF:diskConfig"]))
	assert.JSONEq(t, `[{"name":"default"}]`, string(created.Server["security_groups"]))
	selfLink := instance.URL + projectPath + "/servers/" + serverID
	assert.JSONEq(t, `[{"rel":"self","href":"`+selfLink+`"},`+
		`{"rel":"bookmark","href":"`+instance.URL+"/compute/"+projectID+"/servers/"+serverID+`"}]`,
		string(created.Server["links"]))
	assert.Equal(t, selfLink, headers.Get("Location"))
	requestID := headers.Get("X-Openstack-Request-Id")

	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/views/servers.py#L263-L345
	status, _, body = request(t, instance, http.MethodGet, projectPath+"/servers/"+serverID, "", auth)

	require.Equal(t, http.StatusOK, status, "%s", body)
	detail := decodeWireComputeServer(t, body)
	assert.Equal(t, "ACTIVE", detail.Status)
	assert.Equal(t, "active", detail.VMState)
	var fields wireCreatedServer
	require.NoError(t, json.Unmarshal(body, &fields))
	assert.JSONEq(t, `null`, string(fields.Server["key_name"]))
	assert.JSONEq(t, `[{"name":"default"}]`, string(fields.Server["security_groups"]))
	attached := serverPorts(t, clients.network, serverID)
	require.Len(t, attached, 1)
	assert.JSONEq(t, `{"fixture-network":[{"version":4,"addr":"`+attached[0].FixedIPs[0].IPAddress+
		`","OS-EXT-IPS:type":"fixed","OS-EXT-IPS-MAC:mac_addr":"`+attached[0].MACAddress+`"}]}`,
		string(fields.Server["addresses"]))
	status, _, body = request(t, instance, http.MethodGet,
		projectPath+"/servers/"+serverID+"/os-instance-actions/"+requestID, "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	assert.Contains(t, string(body), `"action":"create"`)

	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/network/neutron.py#L2523-L2541
	_, err = networks.Create(ctx, clients.network, networks.CreateOpts{Name: "second"}).Extract()
	require.NoError(t, err)
	status, headers, body = create(serverCreateBody)

	requireNovaFault(t, http.StatusConflict, "conflictingRequest", status, headers, body)
	status, headers, body = create(withServer(`,"networks":[{"port":"` + attached[0].ID + `"}]`))
	requireNovaFault(t, http.StatusConflict, "conflictingRequest", status, headers, body)
	assert.Contains(t, string(body), "Port "+attached[0].ID+" is still in use.")

	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L2818-L2824
	serverPath := projectPath + "/servers/" + serverID
	status, _, body = request(t, instance, http.MethodPost, serverPath+"/action", `{"lock":null}`, auth)
	require.Equal(t, http.StatusAccepted, status, "%s", body)
	status, headers, body = request(t, instance, http.MethodDelete, serverPath, "", auth)
	requireNovaFault(t, http.StatusConflict, "conflictingRequest", status, headers, body)
	status, _, body = request(t, instance, http.MethodPost, serverPath+"/action", `{"unlock":null}`, auth)
	require.Equal(t, http.StatusAccepted, status, "%s", body)

	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/servers.py#L925-L941
	status, _, body = request(t, instance, http.MethodDelete, computePath+"/servers/"+serverID, "", auth)

	require.Equal(t, http.StatusNoContent, status, "%s", body)
	assert.Empty(t, body)
	status, headers, body = request(t, instance, http.MethodGet, serverPath, "", auth)
	requireNovaFault(t, http.StatusNotFound, "itemNotFound", status, headers, body)
	status, headers, body = request(t, instance, http.MethodDelete, serverPath, "", auth)
	requireNovaFault(t, http.StatusNotFound, "itemNotFound", status, headers, body)
	status, _, body = request(t, instance, http.MethodGet,
		networkPath+"/v2.0/ports/"+attached[0].ID, "", auth)
	assert.Equal(t, http.StatusNotFound, status, "%s", body)
}

// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/views/servers.py#L609-L619
func TestNovaServerBuildControlsWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	clients := newBootClients(t, instance)
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	admin := map[string]string{"X-SandStack-Admin-Token": adminToken}
	_, err := subnets.Create(t.Context(), clients.network, subnets.CreateOpts{
		NetworkID: fixtureNetworkID, CIDR: "10.20.0.0/24", IPVersion: gophercloud.IPv4,
	}).Extract()
	require.NoError(t, err)
	createServer := func() string {
		t.Helper()
		status, _, body := request(t, instance, http.MethodPost, computePath+"/servers", serverCreateBody, auth)
		require.Equal(t, http.StatusAccepted, status, "%s", body)
		var created struct {
			Server struct {
				ID string `json:"id"`
			} `json:"server"`
		}
		require.NoError(t, json.Unmarshal(body, &created))
		return created.Server.ID
	}
	read := func(id string) (wireComputeServer, wireCreatedServer) {
		t.Helper()
		status, _, body := request(t, instance, http.MethodGet, computePath+"/servers/"+id, "", auth)
		require.Equal(t, http.StatusOK, status, "%s", body)
		var fields wireCreatedServer
		require.NoError(t, json.Unmarshal(body, &fields))
		return decodeWireComputeServer(t, body), fields
	}
	control := func(method, path string) (int, []byte) {
		t.Helper()
		status, _, body := request(t, instance, method, "/_sandstack/compute"+path, "", admin)
		return status, body
	}
	status, body := control(http.MethodPut, "/hold-builds")
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	completed := createServer()
	failed := createServer()

	building, fields := read(completed)

	assert.Equal(t, "BUILD", building.Status)
	assert.Equal(t, "building", building.VMState)
	assert.JSONEq(t, `"scheduling"`, string(building.TaskState))
	require.NotNil(t, building.PowerState)
	assert.Equal(t, 0, *building.PowerState)
	assert.JSONEq(t, `{}`, string(fields.Server["addresses"]))
	assert.Len(t, listComputeServers(t, clients.compute, "BUILD"), 2)

	status, body = control(http.MethodPut, "/servers/"+completed+"/complete-create")
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	status, body = control(http.MethodPut, "/servers/"+failed+"/fail-create")
	require.Equal(t, http.StatusNoContent, status, "%s", body)

	waitForServer(t, clients.compute, completed, "ACTIVE")
	active, fields := read(completed)
	assert.Equal(t, "active", active.VMState)
	assert.Contains(t, string(fields.Server["addresses"]), "fixture-network")
	errored, _ := read(failed)
	assert.Equal(t, "ERROR", errored.Status)
	assert.Contains(t, string(errored.Fault), "No valid host was found. There are not enough hosts available.")
	status, body = control(http.MethodPut, "/servers/"+completed+"/complete-create")
	assert.Equal(t, http.StatusNoContent, status, "%s", body)
	status, body = control(http.MethodPut, "/servers/"+completed+"/fail-create")
	assert.Equal(t, http.StatusConflict, status, "%s", body)
	status, body = control(http.MethodPut, "/servers/00000000-0000-4000-8000-000000000000/complete-create")
	assert.Equal(t, http.StatusNotFound, status, "%s", body)
	status, body = control(http.MethodDelete, "/hold-builds")
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	immediateID := createServer()
	immediate, _ := read(immediateID)
	assert.Equal(t, "ACTIVE", immediate.Status)
	status, body = control(http.MethodPut, "/servers/"+immediateID+"/fault")
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	faulted, _ := read(immediateID)
	assert.Equal(t, "ERROR", faulted.Status)
	require.NoError(t, servers.Delete(t.Context(), clients.compute, failed).ExtractErr())
	require.NoError(t, servers.Delete(t.Context(), clients.compute, fixtureServerID).ExtractErr())

	status, _, body = request(t, instance, http.MethodPost, "/_sandstack/reset", "", admin)

	require.Equal(t, http.StatusNoContent, status, "%s", body)
	compute := newBootClients(t, instance).compute
	remaining := listComputeServers(t, compute, "")
	require.Len(t, remaining, 1)
	assert.Equal(t, fixtureServerID, remaining[0].ID)
}

func keysOf(fields map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	return keys
}
