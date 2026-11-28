package server_test

import (
	"net/http"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/ports"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/subnets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGophercloudReadsExternalSubnet(t *testing.T) {
	instance := startServer(t, "")
	client := subnetListClient(t, authenticate(t, instance))
	ctx := t.Context()

	detail, err := subnets.Get(ctx, client, externalSubnetID).Extract()
	require.NoError(t, err)
	pages, err := subnets.List(client, subnets.ListOpts{NetworkID: externalNetworkID}).AllPages(ctx)
	require.NoError(t, err)
	listed, err := subnets.ExtractSubnets(pages)
	require.NoError(t, err)

	assert.Equal(t, externalNetworkID, detail.NetworkID)
	assert.Equal(t, externalProjectID, detail.ProjectID)
	assert.Equal(t, "public-subnet", detail.Name)
	assert.Equal(t, "203.0.113.0/24", detail.CIDR)
	assert.Equal(t, "203.0.113.1", detail.GatewayIP)
	assert.Equal(t, []subnets.Subnet{*detail}, listed)
	name := "taken"
	_, err = subnets.Update(ctx, client, externalSubnetID, subnets.UpdateOpts{Name: &name}).Extract()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusForbidden), "%v", err)
	err = subnets.Delete(ctx, client, externalSubnetID).ExtractErr()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusForbidden), "%v", err)
	_, err = ports.Create(ctx, client, ports.CreateOpts{
		NetworkID: externalNetworkID, FixedIPs: []ports.IP{{SubnetID: externalSubnetID}},
	}).Extract()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusNotFound), "%v", err)
	unchanged, err := subnets.Get(ctx, client, externalSubnetID).Extract()
	require.NoError(t, err)
	assert.Equal(t, detail, unchanged)
}

func TestExternalSubnetMutationWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	detail := subnetsPath + "/" + externalSubnetID

	updateStatus, updateHeaders, updateBody := request(t, instance, http.MethodPut, detail,
		`{"subnet":{"name":"taken"}}`, auth)
	deleteStatus, deleteHeaders, deleteBody := request(t, instance, http.MethodDelete, detail, "", auth)
	readStatus, _, readBody := request(t, instance, http.MethodGet, detail, "", auth)

	assert.Equal(t, http.StatusForbidden, updateStatus, "%s", updateBody)
	requireJSON(t, updateHeaders)
	assertPortNeutronError(t, updateBody, "PolicyNotAuthorized")
	assert.Equal(t, http.StatusForbidden, deleteStatus, "%s", deleteBody)
	requireJSON(t, deleteHeaders)
	assertPortNeutronError(t, deleteBody, "PolicyNotAuthorized")
	require.Equal(t, http.StatusOK, readStatus, "%s", readBody)
	assert.Contains(t, string(readBody), `"name":"public-subnet"`)
}
