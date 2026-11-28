package server_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/subnets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGophercloudManagesSubnetDNSNameservers(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	client, err := openstack.NewNetworkV2(provider, gophercloud.EndpointOpts{
		Region: "RegionOne", Availability: gophercloud.AvailabilityPublic,
	})
	require.NoError(t, err)
	ctx := t.Context()
	initialDNS := []string{"203.0.113.53", "192.0.2.53"}

	created, err := subnets.Create(ctx, client, subnets.CreateOpts{
		Name: "dns-subnet", NetworkID: fixtureNetworkID, CIDR: "10.69.0.0/24",
		IPVersion: gophercloud.IPv4, DNSNameservers: initialDNS,
	}).Extract()

	require.NoError(t, err)
	require.NotEmpty(t, created.ID)
	assert.Equal(t, initialDNS, created.DNSNameservers)
	checkRead := func(want subnets.Subnet) {
		t.Helper()
		found, err := subnets.Get(ctx, client, created.ID).Extract()
		require.NoError(t, err)
		assert.Equal(t, &want, found)
		pages, err := subnets.List(client, subnets.ListOpts{NetworkID: fixtureNetworkID}).AllPages(ctx)
		require.NoError(t, err)
		listed, err := subnets.ExtractSubnets(pages)
		require.NoError(t, err)
		assert.Equal(t, []subnets.Subnet{want}, listed)
	}
	checkRead(*created)

	// Neutron stores DNS addresses with their request order during create and update.
	// https://github.com/openstack/neutron/blob/master/neutron/db/ipam_backend_mixin.py#L140-L153
	replacement := []string{"192.0.2.53", "198.51.100.53", "192.0.2.54"}
	revised, err := subnets.Update(ctx, client, created.ID, subnets.UpdateOpts{
		DNSNameservers: &replacement,
	}).Extract()

	require.NoError(t, err)
	want := *created
	want.DNSNameservers = replacement
	assert.Equal(t, &want, revised)
	checkRead(want)

	name := "renamed-dns-subnet"
	revised, err = subnets.Update(ctx, client, created.ID, subnets.UpdateOpts{Name: &name}).Extract()
	require.NoError(t, err)
	want.Name = name
	assert.Equal(t, &want, revised)

	bad := []string{"192.0.2.54", "2001:db8::53"}
	unappliedName := "must-not-change"
	_, err = subnets.Update(ctx, client, created.ID, subnets.UpdateOpts{
		Name: &unappliedName, DNSNameservers: &bad,
	}).Extract()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusBadRequest), "IPv6 DNS server on IPv4 subnet: %v", err)
	checkRead(want)

	empty := []string{}
	cleared, err := subnets.Update(ctx, client, created.ID, subnets.UpdateOpts{
		DNSNameservers: &empty,
	}).Extract()

	require.NoError(t, err)
	want.DNSNameservers = empty
	assert.Equal(t, &want, cleared)
	checkRead(want)
}

func TestSubnetDNSNameserversWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	const cidr = "10.69.0.0/24"
	createBody := func(dns string) string {
		return fmt.Sprintf(`{"subnet":{"network_id":%q,"ip_version":4,"cidr":%q,"dns_nameservers":%s}}`,
			fixtureNetworkID, cidr, dns)
	}
	read := func(body []byte) wireSubnet {
		t.Helper()
		var response struct {
			Subnet wireSubnet `json:"subnet"`
		}
		require.NoError(t, json.Unmarshal(body, &response))
		require.NotEmpty(t, response.Subnet.ID)
		require.NotEmpty(t, response.Subnet.DNSNameservers)
		return response.Subnet
	}

	// https://docs.openstack.org/api-ref/network/v2/#create-subnet
	status, headers, body := request(t, instance, http.MethodPost, subnetsPath, createBody(`["2001:db8::53"]`), auth)
	require.Equal(t, http.StatusBadRequest, status, "%s", body)
	requireJSON(t, headers)
	assertPortNeutronError(t, body, "InvalidInput")
	status, headers, body = request(t, instance, http.MethodGet, subnetsPath+"?network_id="+fixtureNetworkID, "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	assert.JSONEq(t, `{"subnets":[]}`, string(body))

	const initial = `["203.0.113.53", "192.0.2.53"]`
	status, headers, body = request(t, instance, http.MethodPost, subnetsPath, createBody(initial), auth)
	require.Equal(t, http.StatusCreated, status, "%s", body)
	requireJSON(t, headers)
	created := read(body)
	assert.Equal(t, fixtureNetworkID, created.NetworkID)
	assert.Equal(t, cidr, created.CIDR)
	assert.JSONEq(t, initial, string(created.DNSNameservers))
	path := subnetsPath + "/" + created.ID

	// https://docs.openstack.org/api-ref/network/v2/#update-subnet
	for _, invalid := range []struct {
		dns  string
		kind string
	}{
		{`"192.0.2.53"`, "HTTPBadRequest"},
		{`{"ip":"192.0.2.53"}`, "HTTPBadRequest"},
		{`[53]`, "HTTPBadRequest"},
		{`[null]`, "HTTPBadRequest"},
		{`["not-an-ip"]`, "HTTPBadRequest"},
		{`["2001:db8::53"]`, "InvalidInput"},
		{`["192.0.2.53","192.0.2.53"]`, "HTTPBadRequest"},
		{`["192.0.2.53","192.0.2.54","192.0.2.55","192.0.2.56","192.0.2.57","192.0.2.58"]`, "DNSNameServersExhausted"},
	} {
		payload := `{"subnet":{"name":"must-not-change","dns_nameservers":` + invalid.dns + `}}`
		status, headers, body := request(t, instance, http.MethodPut, path, payload, auth)
		require.Equal(t, http.StatusBadRequest, status, "%s: %s", invalid.dns, body)
		requireJSON(t, headers)
		assertPortNeutronError(t, body, invalid.kind)
	}
	status, headers, body = request(t, instance, http.MethodGet, path, "", auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	assert.Equal(t, created, read(body))

	const limit = `["192.0.2.53","192.0.2.54","192.0.2.55","192.0.2.56","192.0.2.57"]`
	status, headers, body = request(t, instance, http.MethodPut, path, `{"subnet":{"dns_nameservers":`+limit+`}}`, auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	assert.JSONEq(t, limit, string(read(body).DNSNameservers))

	// Neutron's subnet schema converts null to an empty list.
	status, headers, body = request(t, instance, http.MethodPut, path, `{"subnet":{"dns_nameservers":null}}`, auth)
	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	cleared := read(body)
	assert.Equal(t, created.ID, cleared.ID)
	assert.JSONEq(t, "[]", string(cleared.DNSNameservers))
}
