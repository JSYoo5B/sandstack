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
	Subnets      []string `json:"subnets"`
}

type networkResponse struct {
	Network networkBody `json:"network"`
}

func describeNetwork(network domain.Network) networkBody {
	return networkBody{
		ID:           network.ID,
		Name:         network.Name,
		ProjectID:    network.ProjectID,
		TenantID:     network.ProjectID,
		Status:       network.Status(),
		AdminStateUp: !network.AdminDown,
		Shared:       false,
		Subnets:      []string{},
	}
}

func (h handler) listNetworks(w http.ResponseWriter, r *http.Request, projectID string) {
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BadRequest", "The network list query is malformed.")
		return
	}
	for field := range query {
		if field != "name" && field != "status" {
			writeError(w, http.StatusBadRequest, "BadRequest", "Network lists accept only name and status query parameters.")
			return
		}
	}
	filter := app.ListFilter{Names: query["name"], Statuses: query["status"]}
	networks := h.service.List(projectID, filter)
	response := struct {
		Networks []networkBody `json:"networks"`
	}{Networks: make([]networkBody, len(networks))}
	for i, network := range networks {
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

func writeNetworkNotFound(w http.ResponseWriter, networkID string) {
	writeError(w, http.StatusNotFound, "NetworkNotFound",
		fmt.Sprintf("Network %s could not be found.", networkID))
}
