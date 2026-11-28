package server_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/servers"
	"github.com/gophercloud/gophercloud/v2/openstack/identity/v3/tokens"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	computePath     = "/compute/v2.1"
	fixtureServerID = "d8ba25c2-8224-4e09-88ef-fab16c654770"
	faultPath       = "/_sandstack/compute/servers/" + fixtureServerID + "/fault"
)

func TestComputeServerDetailsWire(t *testing.T) {
	instance := startServer(t, "")
	status, headers, body := request(t, instance, http.MethodPost, tokensPath, scopedPasswordRequest, nil)
	require.Equal(t, http.StatusCreated, status, "%s", body)
	tokenID := headers.Get("X-Subject-Token")
	require.NotEmpty(t, tokenID)
	var issuance struct {
		Token struct {
			User    namedResource `json:"user"`
			Project namedResource `json:"project"`
		} `json:"token"`
	}
	require.NoError(t, json.Unmarshal(body, &issuance))
	projectID := issuance.Token.Project.ID
	userID := issuance.Token.User.ID
	require.NotEmpty(t, projectID)
	require.NotEmpty(t, userID)
	serversPath := computePath + "/" + projectID + "/servers/"
	serverPath := serversPath + fixtureServerID
	auth := map[string]string{"X-Auth-Token": tokenID}

	// https://docs.openstack.org/api-ref/compute/#show-server-details
	status, headers, body = request(t, instance, http.MethodGet, serverPath, "", auth)

	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	assert.Equal(t, "compute 2.1", headers.Get("OpenStack-API-Version"))
	assert.Equal(t, "2.1", headers.Get("X-OpenStack-Nova-API-Version"))
	server := decodeWireComputeServer(t, body)
	assert.Equal(t, fixtureServerID, server.ID)
	assert.Equal(t, "fixture-server", server.Name)
	assert.Equal(t, projectID, server.ProjectID)
	assert.Equal(t, userID, server.UserID)
	assert.False(t, server.Created.IsZero())
	assert.False(t, server.Updated.Before(server.Created))
	assert.Equal(t, "ACTIVE", server.Status)
	assert.Equal(t, "active", server.VMState)
	require.NotNil(t, server.PowerState)
	assert.Equal(t, 1, *server.PowerState)
	assert.Equal(t, "null", string(server.TaskState))
	assert.Empty(t, server.Fault)
	assert.Contains(t, server.Links, computeLink{Href: instance.URL + serverPath, Rel: "self"})
	assert.NotNil(t, server.Metadata)
	assert.NotNil(t, server.Addresses)
	assert.NotEmpty(t, server.Flavor.ID)
	assert.NotEmpty(t, server.Image.ID)

	projectlessPath := computePath + "/servers/" + fixtureServerID
	status, _, body = request(t, instance, http.MethodGet, projectlessPath, "", auth)

	require.Equal(t, http.StatusOK, status, "%s", body)
	projectless := decodeWireComputeServer(t, body)
	assert.Equal(t, server.ID, projectless.ID)
	assert.Equal(t, projectID, projectless.ProjectID)
	assert.Contains(t, projectless.Links, computeLink{Href: instance.URL + projectlessPath, Rel: "self"})

	status, _, body = request(t, instance, http.MethodPut, faultPath, "", map[string]string{
		"X-SandStack-Admin-Token": adminToken,
	})
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	status, headers, body = request(t, instance, http.MethodGet, serverPath, "", auth)

	require.Equal(t, http.StatusOK, status, "%s", body)
	requireJSON(t, headers)
	failed := decodeWireComputeServer(t, body)
	assert.Equal(t, server.ID, failed.ID)
	assert.Equal(t, "ERROR", failed.Status)
	assert.Equal(t, "error", failed.VMState)
	require.NotNil(t, failed.PowerState)
	assert.Equal(t, 6, *failed.PowerState)
	assert.Equal(t, "null", string(failed.TaskState))
	var fault struct {
		Code    int       `json:"code"`
		Message string    `json:"message"`
		Created time.Time `json:"created"`
	}
	require.NoError(t, json.Unmarshal(failed.Fault, &fault))
	assert.Equal(t, http.StatusInternalServerError, fault.Code)
	assert.Equal(t, "Injected server fault", fault.Message)
	assert.False(t, fault.Created.IsZero())

	for _, tc := range []struct {
		name      string
		path      string
		token     string
		version   string
		status    int
		faultType string
	}{
		{
			name: "invalid token", path: serverPath, token: "invalid-token",
			status: http.StatusUnauthorized, faultType: "unauthorized",
		},
		{
			name: "missing server", path: serversPath + "00000000-0000-0000-0000-000000000000", token: tokenID,
			status: http.StatusNotFound, faultType: "itemNotFound",
		},
		{
			name: "different project", path: computePath + "/another-project/servers/" + fixtureServerID,
			token: tokenID, status: http.StatusBadRequest, faultType: "badRequest",
		},
		{
			name: "unsupported API microversion", path: serverPath, token: tokenID, version: "compute 2.2",
			status: http.StatusNotAcceptable, faultType: "computeFault",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requestHeaders := map[string]string{"X-Auth-Token": tc.token}
			if tc.version != "" {
				requestHeaders["OpenStack-API-Version"] = tc.version
			}
			status, headers, body := request(t, instance, http.MethodGet, tc.path, "", requestHeaders)

			require.Equal(t, tc.status, status, "%s", body)
			requireJSON(t, headers)
			var response map[string]struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			}
			require.NoError(t, json.Unmarshal(body, &response))
			require.Contains(t, response, tc.faultType)
			assert.Equal(t, tc.status, response[tc.faultType].Code)
			assert.NotEmpty(t, response[tc.faultType].Message)
		})
	}
}

func TestGophercloudReadsComputeServerFaultAndRecovery(t *testing.T) {
	instance := startServer(t, "")
	provider := authenticate(t, instance)
	auth, ok := provider.GetAuthResult().(tokens.CreateResult)
	require.True(t, ok)
	project, err := auth.ExtractProject()
	require.NoError(t, err)
	require.NotNil(t, project)
	compute, err := openstack.NewComputeV2(provider, gophercloud.EndpointOpts{
		Region: "RegionOne", Availability: gophercloud.AvailabilityPublic,
	})
	require.NoError(t, err)
	assert.Equal(t, instance.URL+computePath+"/"+project.ID+"/", compute.Endpoint)
	compute.Microversion = "2.1"

	baseline, err := servers.Get(t.Context(), compute, fixtureServerID).Extract()

	require.NoError(t, err)
	assert.Equal(t, fixtureServerID, baseline.ID)
	assert.Equal(t, "fixture-server", baseline.Name)
	assert.Equal(t, project.ID, baseline.TenantID)
	assert.Equal(t, "ACTIVE", baseline.Status)
	assert.Equal(t, servers.Fault{}, baseline.Fault)

	status, _, body := request(t, instance, http.MethodPut, faultPath, "", map[string]string{
		"X-SandStack-Admin-Token": provider.Token(),
	})
	require.Equal(t, http.StatusUnauthorized, status, "%s", body)
	stillActive, err := servers.Get(t.Context(), compute, fixtureServerID).Extract()
	require.NoError(t, err)
	assert.Equal(t, baseline, stillActive)

	control := map[string]string{"X-SandStack-Admin-Token": adminToken}
	status, _, body = request(t, instance, http.MethodPut, faultPath, "", control)
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	faulted, err := servers.Get(t.Context(), compute, fixtureServerID).Extract()

	require.NoError(t, err)
	assert.Equal(t, baseline.ID, faulted.ID)
	assert.Equal(t, baseline.Created, faulted.Created)
	assert.Equal(t, "ERROR", faulted.Status)
	assert.Equal(t, http.StatusInternalServerError, faulted.Fault.Code)
	assert.Equal(t, "Injected server fault", faulted.Fault.Message)
	assert.False(t, faulted.Fault.Created.IsZero())

	status, _, body = request(t, instance, http.MethodPut, faultPath, "", control)
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	stillFaulted, err := servers.Get(t.Context(), compute, fixtureServerID).Extract()
	require.NoError(t, err)
	assert.Equal(t, faulted, stillFaulted)

	status, _, body = request(t, instance, http.MethodDelete, faultPath, "", control)
	require.Equal(t, http.StatusNoContent, status, "%s", body)
	recovered, err := servers.Get(t.Context(), compute, fixtureServerID).Extract()

	require.NoError(t, err)
	assert.Equal(t, baseline.ID, recovered.ID)
	assert.Equal(t, baseline.Created, recovered.Created)
	assert.Equal(t, "ACTIVE", recovered.Status)
	assert.Equal(t, servers.Fault{}, recovered.Fault)
}

type computeLink struct {
	Href string `json:"href"`
	Rel  string `json:"rel"`
}

type wireResourceRef struct {
	ID    string        `json:"id"`
	Links []computeLink `json:"links"`
}

type wireComputeServer struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	ProjectID  string            `json:"tenant_id"`
	UserID     string            `json:"user_id"`
	Created    time.Time         `json:"created"`
	Updated    time.Time         `json:"updated"`
	Status     string            `json:"status"`
	VMState    string            `json:"OS-EXT-STS:vm_state"`
	PowerState *int              `json:"OS-EXT-STS:power_state"`
	TaskState  json.RawMessage   `json:"OS-EXT-STS:task_state"`
	Fault      json.RawMessage   `json:"fault"`
	Links      []computeLink     `json:"links"`
	Metadata   map[string]string `json:"metadata"`
	Addresses  map[string]any    `json:"addresses"`
	Flavor     wireResourceRef   `json:"flavor"`
	Image      wireResourceRef   `json:"image"`
}

func decodeWireComputeServer(t *testing.T, body []byte) wireComputeServer {
	t.Helper()
	var result struct {
		Server wireComputeServer `json:"server"`
	}
	require.NoError(t, json.Unmarshal(body, &result))
	return result.Server
}

func requireNovaFault(t *testing.T, wantStatus int, kind string, status int, headers http.Header, body []byte) {
	t.Helper()
	require.Equal(t, wantStatus, status, "%s", body)
	requireJSON(t, headers)
	var faults map[string]struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	require.NoError(t, json.Unmarshal(body, &faults))
	require.Contains(t, faults, kind)
	assert.Equal(t, wantStatus, faults[kind].Code)
	assert.NotEmpty(t, faults[kind].Message)
}
