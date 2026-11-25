package server_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/servers"
	"github.com/gophercloud/gophercloud/v2/openstack/identity/v3/tokens"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComputeServerListWire(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	authResult, ok := provider.GetAuthResult().(tokens.CreateResult)
	require.True(t, ok)
	project, err := authResult.ExtractProject()
	require.NoError(t, err)
	require.NotNil(t, project)
	require.NotEmpty(t, project.ID)
	auth := map[string]string{"X-Auth-Token": provider.Token()}
	projectBase := computePath + "/" + project.ID
	listPath := projectBase + "/servers/detail"

	// https://docs.openstack.org/api-ref/compute/#list-servers-detailed
	status, headers, body := request(t, instance, http.MethodGet, listPath, "", auth)

	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	assert.Equal(t, "compute 2.1", headers.Get("OpenStack-API-Version"))
	rows := decodeWireComputeList(t, body)
	require.Len(t, rows, 1)
	baseline := rows[0]
	assert.Equal(t, fixtureServerID, baseline.ID)
	assert.Equal(t, "fixture-server", baseline.Name)
	assert.Equal(t, project.ID, baseline.ProjectID)
	assert.Equal(t, "ACTIVE", baseline.Status)
	assert.Equal(t, "active", baseline.VMState)
	assert.Empty(t, baseline.Fault)
	assert.Contains(t, baseline.Links, computeLink{
		Href: instance.URL + projectBase + "/servers/" + baseline.ID, Rel: "self",
	})

	projectlessList := computePath + "/servers/detail"
	status, _, body = request(t, instance, http.MethodGet, projectlessList+"?status=aCtIvE", "", auth)

	require.Equal(t, http.StatusOK, status, "%s", body)
	rows = decodeWireComputeList(t, body)
	require.Len(t, rows, 1)
	assert.Equal(t, baseline.ID, rows[0].ID)
	assert.Equal(t, baseline.ProjectID, rows[0].ProjectID)
	assert.Contains(t, rows[0].Links, computeLink{
		Href: instance.URL + computePath + "/servers/" + rows[0].ID, Rel: "self",
	})

	// The Nova 2.38 history records that earlier versions returned an empty
	// list for a non-admin request with an unrecognized status.
	// https://docs.openstack.org/nova/latest/reference/api-microversion-history.html
	for _, filter := range []string{"ERROR", "invalid_status", ""} {
		status, _, body = request(t, instance, http.MethodGet, listPath+"?status="+filter, "", auth)

		require.Equal(t, http.StatusOK, status, "%s", body)
		assert.Empty(t, decodeWireComputeList(t, body), "status=%q", filter)
	}

	// The list accepts a single status value and rejects unrelated query keys.
	for _, query := range []string{"?limit=1", "?status=ACTIVE&status=ERROR"} {
		status, headers, body = request(t, instance, http.MethodGet, listPath+query, "", auth)

		require.Equal(t, http.StatusBadRequest, status, "%s", body)
		requireJSON(t, headers)
		var response struct {
			BadRequest struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"badRequest"`
		}
		require.NoError(t, json.Unmarshal(body, &response))
		assert.Equal(t, http.StatusBadRequest, response.BadRequest.Code, query)
		assert.NotEmpty(t, response.BadRequest.Message, query)
	}

	status, _, body = request(t, instance, http.MethodGet, listPath, "", map[string]string{
		"X-Auth-Token": "invalid-token",
	})
	assert.Equal(t, http.StatusUnauthorized, status, "%s", body)
}

func TestGophercloudDiscoversAndFiltersComputeServerAcrossFault(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	compute, err := openstack.NewComputeV2(provider, gophercloud.EndpointOpts{
		Region: "RegionOne", Availability: gophercloud.AvailabilityPublic,
	})
	require.NoError(t, err)

	initial := listComputeServers(t, compute, "")

	require.Len(t, initial, 1)
	baseline := initial[0]
	serverID := baseline.ID
	require.NotEmpty(t, serverID)
	assert.Equal(t, "fixture-server", baseline.Name)
	assert.Equal(t, "ACTIVE", baseline.Status)
	assert.Equal(t, servers.Fault{}, baseline.Fault)
	assert.Equal(t, initial, listComputeServers(t, compute, "ACTIVE"))
	assert.Empty(t, listComputeServers(t, compute, "ERROR"))
	baselineDetail, err := servers.Get(t.Context(), compute, serverID).Extract()
	require.NoError(t, err)
	assert.Equal(t, baseline, *baselineDetail)

	controlPath := "/_sandstack/compute/servers/" + serverID + "/fault"
	control := map[string]string{"X-SandStack-Admin-Token": adminToken}
	status, _, body := request(t, instance, http.MethodPut, controlPath, "", control)
	require.Equal(t, http.StatusNoContent, status, "%s", body)

	failed := listComputeServers(t, compute, "ERROR")

	require.Len(t, failed, 1)
	assert.Equal(t, serverID, failed[0].ID)
	assert.Equal(t, "ERROR", failed[0].Status)
	assert.Equal(t, http.StatusInternalServerError, failed[0].Fault.Code)
	assert.Equal(t, "Injected server fault", failed[0].Fault.Message)
	assert.False(t, failed[0].Fault.Created.IsZero())
	assert.Empty(t, listComputeServers(t, compute, "ACTIVE"))
	faultDetail, err := servers.Get(t.Context(), compute, serverID).Extract()
	require.NoError(t, err)
	assert.Equal(t, failed[0], *faultDetail)

	status, _, body = request(t, instance, http.MethodDelete, controlPath, "", control)
	require.Equal(t, http.StatusNoContent, status, "%s", body)

	recovered := listComputeServers(t, compute, "ACTIVE")

	require.Len(t, recovered, 1)
	assert.Equal(t, serverID, recovered[0].ID)
	assert.Equal(t, "ACTIVE", recovered[0].Status)
	assert.Equal(t, baseline.Created, recovered[0].Created)
	assert.Equal(t, servers.Fault{}, recovered[0].Fault)
	assert.Empty(t, listComputeServers(t, compute, "ERROR"))
}

func decodeWireComputeList(t *testing.T, body []byte) []wireComputeServer {
	t.Helper()
	var result struct {
		Servers []wireComputeServer `json:"servers"`
	}
	require.NoError(t, json.Unmarshal(body, &result))
	require.NotNil(t, result.Servers, "expected a servers array in %s", body)
	return result.Servers
}

func listComputeServers(t *testing.T, client *gophercloud.ServiceClient, status string) []servers.Server {
	t.Helper()
	page, err := servers.List(client, servers.ListOpts{Status: status}).AllPages(t.Context())
	require.NoError(t, err)
	entries, err := servers.ExtractServers(page)
	require.NoError(t, err)
	return entries
}
