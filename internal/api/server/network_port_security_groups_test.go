package server_test

import (
	"net/http"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/layer3/routers"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/security/groups"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/ports"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGophercloudSetsNeutronPortSecurityGroupsAtCreation(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	resources := createRouterInterfaceResources(t, provider)
	client := resources.service
	ctx := t.Context()
	groupPages, err := groups.List(client, groups.ListOpts{Name: "default"}).AllPages(ctx)
	require.NoError(t, err)
	defaults, err := groups.ExtractGroups(groupPages)
	require.NoError(t, err)
	require.Len(t, defaults, 1)
	custom, err := groups.Create(ctx, client, groups.CreateOpts{Name: "attached group"}).Extract()
	require.NoError(t, err)
	stateful := false
	stateless, err := groups.Create(ctx, client, groups.CreateOpts{
		Name: "stateless group", Stateful: &stateful,
	}).Extract()
	require.NoError(t, err)

	// https://docs.openstack.org/api-ref/network/v2/#create-port
	// https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/db/securitygroups_db.py
	var nullGroups []string
	cases := []struct {
		name   string
		groups *[]string
		want   []string
	}{
		{"omitted", nil, []string{defaults[0].ID}},
		{"empty", &[]string{}, []string{}},
		{"null", &nullGroups, []string{}},
		{"custom", &[]string{custom.ID}, []string{custom.ID}},
	}
	expected := make(map[string][]string, len(cases)+1)
	var boundPortID string
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			created, err := ports.Create(ctx, client, ports.CreateOpts{
				NetworkID:      resources.networkID,
				Name:           tc.name,
				SecurityGroups: tc.groups,
			}).Extract()
			require.NoError(t, err)
			require.NotEmpty(t, created.ID)
			assert.Equal(t, tc.want, created.SecurityGroups)
			expected[created.ID] = tc.want
			if tc.name == "custom" {
				boundPortID = created.ID
			}
		})
	}
	require.NotEmpty(t, boundPortID)

	assertNeutronFailure := func(err error, status int, kind string) {
		t.Helper()
		var failure gophercloud.ErrUnexpectedResponseCode
		require.ErrorAs(t, err, &failure)
		assert.Equal(t, status, failure.Actual, "%s", failure.Body)
		assertPortNeutronError(t, failure.Body, kind)
	}
	_, err = ports.Create(ctx, client, ports.CreateOpts{
		NetworkID:      resources.networkID,
		SecurityGroups: &[]string{"00000000-0000-0000-0000-000000000000"},
	}).Extract()
	assertNeutronFailure(err, http.StatusNotFound, "SecurityGroupNotFound")
	_, err = ports.Create(ctx, client, ports.CreateOpts{
		NetworkID:      resources.networkID,
		SecurityGroups: &[]string{custom.ID, stateless.ID},
	}).Extract()
	assertNeutronFailure(err, http.StatusConflict, "SecurityGroupConflict")

	interfaceInfo, err := routers.AddInterface(ctx, client, resources.routerID, routers.AddInterfaceOpts{
		SubnetID: resources.subnetID,
	}).Extract()
	require.NoError(t, err)
	expected[interfaceInfo.PortID] = []string{}
	for id, want := range expected {
		read, err := ports.Get(ctx, client, id).Extract()
		require.NoError(t, err)
		assert.Equal(t, want, read.SecurityGroups, "port %s", id)
	}
	portPages, err := ports.List(client, ports.ListOpts{NetworkID: resources.networkID}).AllPages(ctx)
	require.NoError(t, err)
	listed, err := ports.ExtractPorts(portPages)
	require.NoError(t, err)
	actual := make(map[string][]string, len(listed))
	for _, port := range listed {
		actual[port.ID] = port.SecurityGroups
	}
	assert.Equal(t, expected, actual)

	err = groups.Delete(ctx, client, custom.ID).ExtractErr()
	assertNeutronFailure(err, http.StatusConflict, "SecurityGroupInUse")
	_, err = groups.Get(ctx, client, custom.ID).Extract()
	require.NoError(t, err)
	require.NoError(t, ports.Delete(ctx, client, boundPortID).ExtractErr())
	require.NoError(t, groups.Delete(ctx, client, custom.ID).ExtractErr())
	_, err = groups.Get(ctx, client, custom.ID).Extract()
	assertNeutronFailure(err, http.StatusNotFound, "SecurityGroupNotFound")
}
