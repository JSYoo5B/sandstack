package server_test

import (
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/keypairs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	novaImportedKey = "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAAAgQDx8nkQv/zgGgB4rMYmIf+6A4l6Rr+o/6lHBQdW5aYd44bd8J" +
		"ttDCE/F/pNRr0lRE+PiqSPO8nDPHw0010JeMH9gYgnnFlyY3/OcJ02RhIPyyxYpv9FhY+2YiUkpwFOcLImyrxEsYXpD/0d3ac3" +
		"0bNH6Sw9JD9UZHYcpSxsIbECHw== Generated-by-Nova"
	novaImportedFingerprint = "1e:2c:9b:56:79:4b:45:77:f9:ca:7a:98:2c:b0:d5:3c"
)

func TestGophercloudManagesNovaKeyPairs(t *testing.T) {
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
	list := func() []keypairs.KeyPair {
		t.Helper()
		pages, err := keypairs.List(compute, nil).AllPages(ctx)
		require.NoError(t, err)
		pairs, err := keypairs.ExtractKeyPairs(pages)
		require.NoError(t, err)
		return pairs
	}
	assert.Empty(t, list())

	imported, err := keypairs.Create(ctx, compute, keypairs.CreateOpts{
		Name: "imported", PublicKey: novaImportedKey,
	}).Extract()
	require.NoError(t, err)
	generated, err := keypairs.Create(ctx, compute, keypairs.CreateOpts{Name: "generated key"}).Extract()
	require.NoError(t, err)

	assert.Equal(t, "imported", imported.Name)
	assert.Equal(t, novaImportedKey, imported.PublicKey)
	assert.Equal(t, novaImportedFingerprint, imported.Fingerprint)
	assert.Empty(t, imported.PrivateKey)
	assert.NotEmpty(t, imported.UserID)
	assert.Empty(t, imported.Type)
	assert.Equal(t, imported.UserID, generated.UserID)
	assert.True(t, strings.HasPrefix(generated.PublicKey, "ssh-rsa "))
	assert.True(t, strings.HasSuffix(generated.PublicKey, " Generated-by-Nova"))
	block, _ := pem.Decode([]byte(generated.PrivateKey))
	require.NotNil(t, block)
	_, err = x509.ParsePKCS1PrivateKey(block.Bytes)
	assert.NoError(t, err)
	assert.Equal(t, []keypairs.KeyPair{
		{Name: generated.Name, Fingerprint: generated.Fingerprint, PublicKey: generated.PublicKey},
		{Name: imported.Name, Fingerprint: imported.Fingerprint, PublicKey: imported.PublicKey},
	}, list())
	read, err := keypairs.Get(ctx, compute, "generated key", nil).Extract()
	require.NoError(t, err)
	assert.Equal(t, keypairs.KeyPair{
		Name: generated.Name, Fingerprint: generated.Fingerprint, PublicKey: generated.PublicKey,
		UserID: generated.UserID,
	}, *read)

	_, duplicateErr := keypairs.Create(ctx, compute, keypairs.CreateOpts{Name: "imported"}).Extract()
	_, invalidErr := keypairs.Create(ctx, compute, keypairs.CreateOpts{
		Name: "invalid", PublicKey: "ssh-rsa invalid",
	}).Extract()
	_, typedErr := keypairs.Create(ctx, compute, keypairs.CreateOpts{Name: "typed", Type: "ssh"}).Extract()

	assert.True(t, gophercloud.ResponseCodeIs(duplicateErr, http.StatusConflict), "%v", duplicateErr)
	assert.True(t, gophercloud.ResponseCodeIs(invalidErr, http.StatusBadRequest), "%v", invalidErr)
	assert.True(t, gophercloud.ResponseCodeIs(typedErr, http.StatusBadRequest), "%v", typedErr)

	require.NoError(t, keypairs.Delete(ctx, compute, "imported", nil).ExtractErr())

	_, err = keypairs.Get(ctx, compute, "imported", nil).Extract()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusNotFound), "%v", err)
	err = keypairs.Delete(ctx, compute, "imported", nil).ExtractErr()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusNotFound), "%v", err)
	require.Len(t, list(), 1)

	status, _, body := request(t, instance, http.MethodPost, "/_sandstack/reset", "",
		map[string]string{"X-SandStack-Admin-Token": adminToken})
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	compute = newCompute()

	assert.Empty(t, list())
	_, err = keypairs.Get(ctx, compute, "generated key", nil).Extract()
	assert.True(t, gophercloud.ResponseCodeIs(err, http.StatusNotFound), "%v", err)
}

func TestNovaKeyPairsWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	collection := computePath + "/" + routerProjectID(t, provider) + "/os-keypairs"
	auth := map[string]string{"X-Auth-Token": provider.Token(), "OpenStack-API-Version": "compute 2.1"}
	decode := func(body []byte) map[string]json.RawMessage {
		t.Helper()
		var envelope struct {
			KeyPair map[string]json.RawMessage `json:"keypair"`
		}
		require.NoError(t, json.Unmarshal(body, &envelope))
		return envelope.KeyPair
	}
	keys := func(fields map[string]json.RawMessage) []string {
		names := make([]string, 0, len(fields))
		for name := range fields {
			names = append(names, name)
		}
		return names
	}

	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/schemas/keypairs.py#L21-L36
	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/validation/parameter_types.py#L298-L308
	for _, body := range []string{
		`{}`,
		`{"keypair":{}}`,
		`{"keypair":{"name":""}}`,
		`{"keypair":{"name":"` + strings.Repeat("k", 256) + `"}}`,
		`{"keypair":{"name":"dotted.name"}}`,
		`{"keypair":{"name":" leading"}}`,
		`{"keypair":{"name":"trailing "}}`,
		`{"keypair":{"name":1}}`,
		`{"keypair":{"name":"key","type":"ssh"}}`,
		`{"keypair":{"name":"key","user_id":"other"}}`,
		`{"keypair":{"name":"key","public_key":null}}`,
		`{"keypair":{"name":"key"},"other":true}`,
		`{"keypair":{"name":"key","name":"key"}}`,
	} {
		status, headers, response := request(t, instance, http.MethodPost, collection, body, auth)
		requireNovaFault(t, http.StatusBadRequest, "badRequest", status, headers, response)
	}

	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/crypto.py#L68-L81
	status, headers, body := request(t, instance, http.MethodPost, collection,
		`{"keypair":{"name":"bad","public_key":"ssh-rsa AAAA"}}`, auth)
	requireNovaFault(t, http.StatusBadRequest, "badRequest", status, headers, body)

	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/keypairs.py#L44-L112
	status, headers, body = request(t, instance, http.MethodPost, collection,
		`{"keypair":{"name":"`+strings.Repeat("k", 255)+`","public_key":"`+novaImportedKey+`"}}`, auth)

	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	assert.Equal(t, "compute 2.1", headers.Get("OpenStack-API-Version"))
	created := decode(body)
	assert.ElementsMatch(t, []string{"fingerprint", "name", "public_key", "user_id"}, keys(created))
	assert.JSONEq(t, strconv.Quote(novaImportedFingerprint), string(created["fingerprint"]))
	var userID string
	require.NoError(t, json.Unmarshal(created["user_id"], &userID))
	assert.NotEmpty(t, userID)

	status, _, body = request(t, instance, http.MethodPost, collection, `{"keypair":{"name":"_generated - 1"}}`, auth)

	require.Equal(t, http.StatusOK, status, "%s", body)
	generated := decode(body)
	assert.ElementsMatch(t, []string{"fingerprint", "name", "public_key", "private_key", "user_id"}, keys(generated))
	assert.Contains(t, string(generated["private_key"]), `-----BEGIN RSA PRIVATE KEY-----\n`)
	assert.Regexp(t, `^"([0-9a-f]{2}:){15}[0-9a-f]{2}"$`, string(generated["fingerprint"]))

	status, headers, body = request(t, instance, http.MethodPost, collection,
		`{"keypair":{"name":"_generated - 1","public_key":"`+novaImportedKey+`"}}`, auth)
	requireNovaFault(t, http.StatusConflict, "conflictingRequest", status, headers, body)

	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/views/keypairs.py#L24-L74
	status, headers, body = request(t, instance, http.MethodGet, collection+"?user_id=ignored", "", auth)

	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	var listed struct {
		KeyPairs []struct {
			KeyPair map[string]json.RawMessage `json:"keypair"`
		} `json:"keypairs"`
	}
	require.NoError(t, json.Unmarshal(body, &listed))
	require.Len(t, listed.KeyPairs, 2)
	assert.JSONEq(t, `"_generated - 1"`, string(listed.KeyPairs[0].KeyPair["name"]))
	for _, item := range listed.KeyPairs {
		assert.ElementsMatch(t, []string{"fingerprint", "name", "public_key"}, keys(item.KeyPair))
	}

	status, headers, body = request(t, instance, http.MethodGet, collection+"/_generated%20-%201", "", auth)

	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	shown := decode(body)
	assert.ElementsMatch(t, []string{
		"created_at", "deleted", "deleted_at", "fingerprint", "id", "name", "public_key", "updated_at", "user_id",
	}, keys(shown))
	assert.JSONEq(t, "2", string(shown["id"]))
	assert.JSONEq(t, "false", string(shown["deleted"]))
	assert.JSONEq(t, "null", string(shown["deleted_at"]))
	assert.JSONEq(t, "null", string(shown["updated_at"]))
	assert.Regexp(t, `^"\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{6}"$`, string(shown["created_at"]))
	assert.Equal(t, string(generated["public_key"]), string(shown["public_key"]))

	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/api/openstack/compute/keypairs.py#L119-L144
	projectless := computePath + "/os-keypairs/_generated%20-%201"
	status, headers, body = request(t, instance, http.MethodDelete, projectless, "", auth)

	require.Equal(t, http.StatusAccepted, status, "%s", body)
	assert.Empty(t, body)
	assert.Equal(t, "2.1", headers.Get("X-OpenStack-Nova-API-Version"))
	status, headers, body = request(t, instance, http.MethodDelete, projectless, "", auth)
	requireNovaFault(t, http.StatusNotFound, "itemNotFound", status, headers, body)
	status, headers, body = request(t, instance, http.MethodGet, projectless, "", auth)
	requireNovaFault(t, http.StatusNotFound, "itemNotFound", status, headers, body)
	status, _, body = request(t, instance, http.MethodGet, collection, "", nil)
	assert.Equal(t, http.StatusUnauthorized, status, "%s", body)
}

func TestNovaKeyPairQuotaWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	collection := computePath + "/os-keypairs"
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	for i := range 100 {
		status, _, body := request(t, instance, http.MethodPost, collection,
			`{"keypair":{"name":"key-`+strconv.Itoa(i)+`","public_key":"`+novaImportedKey+`"}}`, auth)
		require.Equal(t, http.StatusOK, status, "%s", body)
	}

	// https://github.com/openstack/nova/blob/18898d20148c48b4671dbae0a461d723859380c5/nova/compute/api.py#L7000-L7009
	status, headers, body := request(t, instance, http.MethodPost, collection,
		`{"keypair":{"name":"key-0","public_key":"invalid"}}`, auth)

	requireNovaFault(t, http.StatusForbidden, "forbidden", status, headers, body)
	status, _, body = request(t, instance, http.MethodDelete, collection+"/key-0", "", auth)
	require.Equal(t, http.StatusAccepted, status, "%s", body)
	status, _, body = request(t, instance, http.MethodPost, collection, `{"keypair":{"name":"key-0"}}`, auth)
	assert.Equal(t, http.StatusOK, status, "%s", body)
}
