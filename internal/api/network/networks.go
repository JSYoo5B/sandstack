package network

import (
	"errors"
	"fmt"
	"net/http"

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

func describeNetwork(network domain.Network) networkBody {
	status := "ACTIVE"
	if network.Down {
		status = "DOWN"
	}
	return networkBody{
		ID:           network.ID,
		Name:         network.Name,
		ProjectID:    network.ProjectID,
		TenantID:     network.ProjectID,
		Status:       status,
		AdminStateUp: true,
		Shared:       false,
		Subnets:      []string{},
	}
}

func (h handler) listNetworks(w http.ResponseWriter, _ *http.Request, projectID string) {
	networks := h.service.List(projectID)
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
		writeError(w, http.StatusNotFound, "NetworkNotFound", fmt.Sprintf("Network %s could not be found.", networkID))
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "HTTPInternalServerError", "Unable to read the network.")
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Network networkBody `json:"network"`
	}{Network: describeNetwork(network)})
}
