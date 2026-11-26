package server_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/layer3/routers"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/security/groups"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/ports"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGophercloudUpdatesNeutronPortSecurityGroups(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	resources := createRouterInterfaceResources(t, provider)
	client := resources.service
	ctx := t.Context()
	original, err := ports.Create(ctx, client, ports.CreateOpts{
		NetworkID: resources.networkID, Name: "original port",
	}).Extract()
	require.NoError(t, err)
	require.Len(t, original.SecurityGroups, 1)
	require.Len(t, original.FixedIPs, 1)
	custom, err := groups.Create(ctx, client, groups.CreateOpts{Name: "extra group"}).Extract()
	require.NoError(t, err)
	stateful := false
	stateless, err := groups.Create(ctx, client, groups.CreateOpts{
		Name: "stateless group", Stateful: &stateful,
	}).Extract()
	require.NoError(t, err)

	update := func(opts ports.UpdateOpts) *ports.Port {
		t.Helper()
		port, err := ports.Update(ctx, client, original.ID, opts).Extract()
		require.NoError(t, err)
		return port
	}
	read := func() *ports.Port {
		t.Helper()
		port, err := ports.Get(ctx, client, original.ID).Extract()
		require.NoError(t, err)
		return port
	}

	// https://docs.openstack.org/api-ref/network/v2/#update-port
	groupIDs := []string{original.SecurityGroups[0], custom.ID}
	expanded := update(ports.UpdateOpts{SecurityGroups: &groupIDs})
	assert.ElementsMatch(t, groupIDs, expanded.SecurityGroups)
	assert.Equal(t, original.FixedIPs, expanded.FixedIPs)
	revisedName := "keeps bindings"
	retained := update(ports.UpdateOpts{Name: &revisedName})
	assert.Equal(t, revisedName, retained.Name)
	assert.ElementsMatch(t, groupIDs, retained.SecurityGroups)
	assert.Equal(t, original.FixedIPs, retained.FixedIPs)

	rejectedName := "must not be written"
	candidateAddress := []ports.IP{{SubnetID: resources.subnetID, IPAddress: "10.66.0.50"}}
	for _, tc := range []struct {
		name     string
		groupIDs []string
		status   int
		kind     string
	}{
		{
			name: "unknown group", groupIDs: []string{"00000000-0000-0000-0000-000000000000"},
			status: http.StatusNotFound, kind: "SecurityGroupNotFound",
		},
		{
			name: "mixed group modes", groupIDs: []string{custom.ID, stateless.ID},
			status: http.StatusConflict, kind: "SecurityGroupConflict",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ports.Update(ctx, client, original.ID, ports.UpdateOpts{
				Name: &rejectedName, FixedIPs: candidateAddress, SecurityGroups: &tc.groupIDs,
			}).Extract()
			var failure gophercloud.ErrUnexpectedResponseCode
			require.ErrorAs(t, err, &failure)
			assert.Equal(t, tc.status, failure.Actual, "%s", failure.Body)
			assertPortNeutronError(t, failure.Body, tc.kind)
			assert.Equal(t, retained, read())
		})
	}
	candidate, err := ports.Create(ctx, client, ports.CreateOpts{
		NetworkID: resources.networkID, FixedIPs: candidateAddress,
	}).Extract()
	require.NoError(t, err)
	assert.Equal(t, candidateAddress, candidate.FixedIPs)

	narrowed := update(ports.UpdateOpts{SecurityGroups: &[]string{custom.ID}})
	assert.Equal(t, []string{custom.ID}, narrowed.SecurityGroups)
	assert.True(t, gophercloud.ResponseCodeIs(
		groups.Delete(ctx, client, custom.ID).ExtractErr(), http.StatusConflict,
	))
	cleared := update(ports.UpdateOpts{SecurityGroups: &[]string{}})
	assert.Equal(t, []string{}, cleared.SecurityGroups)
	assert.Equal(t, cleared, read())
	rebound := update(ports.UpdateOpts{SecurityGroups: &[]string{custom.ID}})
	assert.Equal(t, []string{custom.ID}, rebound.SecurityGroups)

	auth := map[string]string{"X-Auth-Token": provider.Token()}
	path := portsPath + "/" + original.ID
	status, headers, body := request(t, instance, http.MethodPut, path, `{"port":{"security_groups":null}}`, auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	var wire struct {
		Port struct {
			SecurityGroups json.RawMessage `json:"security_groups"`
		} `json:"port"`
	}
	require.NoError(t, json.Unmarshal(body, &wire))
	assert.JSONEq(t, "[]", string(wire.Port.SecurityGroups))

	require.NoError(t, groups.Delete(ctx, client, custom.ID).ExtractErr())
	_, err = groups.Get(ctx, client, custom.ID).Extract()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusNotFound), "%v", err)
	assert.Equal(t, cleared, read())
	pages, err := ports.List(client, ports.ListOpts{NetworkID: resources.networkID}).AllPages(ctx)
	require.NoError(t, err)
	listed, err := ports.ExtractPorts(pages)
	require.NoError(t, err)
	assert.ElementsMatch(t, []ports.Port{*cleared, *candidate}, listed)
}

func TestGophercloudUpdatesSecurityGroupsOnlyOnCapableNeutronRouterPorts(t *testing.T) {
	instance := startServer(t, "")
	resources := createRouterInterfaceResources(t, authenticate(t, instance))
	client := resources.service
	ctx := t.Context()
	group, err := groups.Create(ctx, client, groups.CreateOpts{Name: "interface group"}).Extract()
	require.NoError(t, err)
	assign := ports.UpdateOpts{SecurityGroups: &[]string{group.ID}}

	generated, err := routers.AddInterface(ctx, client, resources.routerID, routers.AddInterfaceOpts{
		SubnetID: resources.subnetID,
	}).Extract()
	require.NoError(t, err)
	_, err = ports.Update(ctx, client, generated.PortID, assign).Extract()
	var failure gophercloud.ErrUnexpectedResponseCode
	require.ErrorAs(t, err, &failure)
	assert.Equal(t, http.StatusBadRequest, failure.Actual, "%s", failure.Body)
	assertPortNeutronError(t, failure.Body, "PortSecurityAndIPRequiredForSecurityGroups")
	cleared, err := ports.Update(ctx, client, generated.PortID, ports.UpdateOpts{
		SecurityGroups: &[]string{},
	}).Extract()
	require.NoError(t, err)
	assert.Equal(t, []string{}, cleared.SecurityGroups)
	_, err = routers.RemoveInterface(ctx, client, resources.routerID, routers.RemoveInterfaceOpts{
		SubnetID: resources.subnetID,
	}).Extract()
	require.NoError(t, err)

	ordinary, err := ports.Create(ctx, client, ports.CreateOpts{NetworkID: resources.networkID}).Extract()
	require.NoError(t, err)
	adopted, err := routers.AddInterface(ctx, client, resources.routerID, routers.AddInterfaceOpts{
		PortID: ordinary.ID,
	}).Extract()
	require.NoError(t, err)
	require.Equal(t, ordinary.ID, adopted.PortID)
	before, err := ports.Get(ctx, client, adopted.PortID).Extract()
	require.NoError(t, err)
	assert.Equal(t, ordinary.SecurityGroups, before.SecurityGroups)
	updated, err := ports.Update(ctx, client, adopted.PortID, assign).Extract()
	require.NoError(t, err)
	assert.Equal(t, []string{group.ID}, updated.SecurityGroups)
	assert.Equal(t, resources.routerID, updated.DeviceID)
	assert.Equal(t, "network:router_interface", updated.DeviceOwner)
	assert.Equal(t, ordinary.FixedIPs, updated.FixedIPs)
}
