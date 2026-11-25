package server_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/servers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequestJournalRecordsRoutesWithoutRequestData(t *testing.T) {
	const journalPath = "/_sandstack/requests"
	instance := startServer(t, "")
	admin := map[string]string{"X-SandStack-Admin-Token": adminToken}
	type journalEntry struct {
		Sequence int    `json:"sequence"`
		Method   string `json:"method"`
		Route    string `json:"route"`
		Status   int    `json:"status"`
	}
	readJournal := func() ([]journalEntry, string) {
		t.Helper()
		status, headers, body := request(t, instance, http.MethodGet, journalPath, "", admin)
		require.Equal(t, http.StatusOK, status, "%s", body)
		requireJSON(t, headers)
		var response struct {
			Requests []journalEntry `json:"requests"`
		}
		decoder := json.NewDecoder(bytes.NewReader(body))
		decoder.DisallowUnknownFields()
		require.NoError(t, decoder.Decode(&response))
		require.NotNil(t, response.Requests, "expected a requests array in %s", body)
		return response.Requests, string(body)
	}

	for _, credentials := range []map[string]string{nil, {"X-SandStack-Admin-Token": "wrong-admin-token"}} {
		status, _, body := request(t, instance, http.MethodGet, journalPath, "", credentials)
		assert.Equal(t, http.StatusUnauthorized, status)
		assert.NotContains(t, string(body), `"requests"`)
	}
	for _, path := range []string{"/_sandstack/health", "/_sandstack/ready"} {
		status, _, _ := request(t, instance, http.MethodGet, path, "", nil)
		require.Equal(t, http.StatusNoContent, status)
	}
	status, _, _ := request(t, instance, http.MethodGet, "/unknown-journal-route", "", nil)
	require.Equal(t, http.StatusNotFound, status)
	initial, _ := readJournal()
	assert.Empty(t, initial)

	provider := authenticate(t, instance)
	compute, err := openstack.NewComputeV2(provider, gophercloud.EndpointOpts{
		Region: "RegionOne", Availability: gophercloud.AvailabilityPublic,
	})
	require.NoError(t, err)
	server, err := servers.Get(t.Context(), compute, fixtureServerID).Extract()
	require.NoError(t, err)
	require.NotEmpty(t, server.TenantID)
	require.Equal(t, fixtureServerID, server.ID)

	const bodySentinel = "journal-example-body-value"
	const querySentinel = "journal-example-query-value"
	const headerSentinel = "journal-example-header-value"
	listPath := computePath + "/" + server.TenantID + "/servers/detail?limit=" + querySentinel
	status, _, _ = request(t, instance, http.MethodGet, listPath, "", map[string]string{
		"X-Auth-Token": provider.Token(),
		"X-Example":    headerSentinel,
	})
	require.Equal(t, http.StatusBadRequest, status)
	wrongPassword := strings.Replace(scopedPasswordRequest, password, bodySentinel, 1)
	status, _, _ = request(t, instance, http.MethodPost, tokensPath, wrongPassword, nil)
	require.Equal(t, http.StatusUnauthorized, status)

	entries, raw := readJournal()

	assert.Equal(t, []journalEntry{
		{Sequence: 1, Method: http.MethodPost, Route: tokensPath, Status: http.StatusCreated},
		{Sequence: 2, Method: http.MethodGet, Route: computePath + "/{projectID}/servers/{serverID}", Status: http.StatusOK},
		{Sequence: 3, Method: http.MethodGet, Route: computePath + "/{projectID}/servers/detail", Status: http.StatusBadRequest},
		{Sequence: 4, Method: http.MethodPost, Route: tokensPath, Status: http.StatusUnauthorized},
	}, entries)
	for _, value := range []string{
		adminToken, provider.Token(), server.TenantID, server.ID, password,
		bodySentinel, querySentinel, headerSentinel,
	} {
		assert.NotContains(t, raw, value)
	}

	status, _, body := request(t, instance, http.MethodPost, "/_sandstack/reset", "", admin)

	require.Equal(t, http.StatusNoContent, status, "%s", body)
	afterReset, _ := readJournal()
	assert.Empty(t, afterReset)
	_, err = servers.Get(t.Context(), compute, fixtureServerID).Extract()
	require.True(t, gophercloud.ResponseCodeIs(err, http.StatusUnauthorized), "%v", err)
	resumed, _ := readJournal()
	assert.Equal(t, []journalEntry{{
		Sequence: 1, Method: http.MethodGet, Route: computePath + "/{projectID}/servers/{serverID}",
		Status: http.StatusUnauthorized,
	}}, resumed)
}
