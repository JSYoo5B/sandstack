package network

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/go-chi/chi/v5"

	app "github.com/JSYoo5B/sandstack/internal/app/network"
	domain "github.com/JSYoo5B/sandstack/internal/domain/network"
)

type networkBody struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	ProjectID    string   `json:"project_id"`
	TenantID     string   `json:"tenant_id"`
	Status       string   `json:"status"`
	AdminStateUp bool     `json:"admin_state_up"`
	Shared       bool     `json:"shared"`
	External     bool     `json:"router:external"`
	Subnets      []string `json:"subnets"`
	PortSecurity bool     `json:"port_security_enabled"`
	Tags         []string `json:"tags"`
}

type networkResponse struct {
	Network networkBody `json:"network"`
}

func describeNetwork(network domain.Network) networkBody {
	subnets := network.SubnetIDs
	if subnets == nil {
		subnets = []string{}
	}
	return networkBody{
		ID:           network.ID,
		Name:         network.Name,
		ProjectID:    network.ProjectID,
		TenantID:     network.ProjectID,
		Status:       network.Status(),
		AdminStateUp: !network.AdminDown,
		Shared:       false,
		External:     network.External,
		Subnets:      subnets,
		PortSecurity: !network.PortSecurityDisabled,
		Tags:         describeTags(network.Tags),
	}
}

func (h handler) listNetworks(w http.ResponseWriter, r *http.Request, projectID string) {
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BadRequest", "The network list query is malformed.")
		return
	}
	for field := range query {
		switch field {
		case "name", "status", "admin_state_up", "router:external", "limit", "marker", "page_reverse",
			"tags", "tags-any", "not-tags", "not-tags-any":
		default:
			writeError(w, http.StatusBadRequest, "BadRequest",
				"Network lists accept only name, status, admin_state_up, router:external, tags, tags-any, "+
					"not-tags, not-tags-any, limit, marker, and page_reverse query parameters.")
			return
		}
	}
	adminStates, ok := parseBooleanFilters(query["admin_state_up"])
	if !ok {
		writeError(w, http.StatusBadRequest, "InvalidInput", "The network list admin_state_up filter requires a boolean.")
		return
	}
	external, ok := parseBooleanFilters(query["router:external"])
	if !ok {
		writeError(w, http.StatusBadRequest, "InvalidInput", "The network list router:external filter requires a boolean.")
		return
	}
	options, err := parsePageOptions(query)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BadRequest", err.Error())
		return
	}
	filter := app.ListFilter{
		Names: query["name"], Statuses: query["status"], AdminStates: adminStates, External: external,
		Tags: parseTagFilter(query),
	}
	page, err := h.service.ListNetworksPage(projectID, filter, options)
	if errors.Is(err, app.ErrNetworkNotFound) {
		writeNetworkNotFound(w, options.Marker)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "HTTPInternalServerError", "Unable to list the networks.")
		return
	}
	response := struct {
		Networks []networkBody `json:"networks"`
		Links    []pageLink    `json:"networks_links,omitempty"`
	}{
		Networks: make([]networkBody, len(page.Items)),
		Links:    h.pageLinks(r, Path+"/networks", query, page.NextMarker, page.PreviousMarker),
	}
	for i, network := range page.Items {
		response.Networks[i] = describeNetwork(network)
	}
	writeJSON(w, http.StatusOK, response)
}

func (h handler) showNetwork(w http.ResponseWriter, r *http.Request, projectID string) {
	networkID := chi.URLParam(r, "networkID")
	network, err := h.service.Get(projectID, networkID)
	if errors.Is(err, app.ErrNetworkNotFound) {
		writeNetworkNotFound(w, networkID)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "HTTPInternalServerError", "Unable to read the network.")
		return
	}
	writeJSON(w, http.StatusOK, networkResponse{Network: describeNetwork(network)})
}

func writeNetworkForbidden(w http.ResponseWriter, action string) {
	writeError(w, http.StatusForbidden, "PolicyNotAuthorized",
		fmt.Sprintf("rule:%s_network is disallowed by policy.", action))
}

func writeNetworkNotFound(w http.ResponseWriter, networkID string) {
	writeError(w, http.StatusNotFound, "NetworkNotFound",
		fmt.Sprintf("Network %s could not be found.", networkID))
}
