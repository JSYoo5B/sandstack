package network

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"

	app "github.com/JSYoo5B/sandstack/internal/app/network"
	domain "github.com/JSYoo5B/sandstack/internal/domain/network"
	"github.com/go-chi/chi/v5"
)

type portBody struct {
	ID           string        `json:"id"`
	Name         string        `json:"name"`
	NetworkID    string        `json:"network_id"`
	ProjectID    string        `json:"project_id"`
	TenantID     string        `json:"tenant_id"`
	MACAddress   string        `json:"mac_address"`
	AdminStateUp bool          `json:"admin_state_up"`
	Status       string        `json:"status"`
	DeviceID     string        `json:"device_id"`
	DeviceOwner  string        `json:"device_owner"`
	FixedIPs     []portFixedIP `json:"fixed_ips"`
}

type portFixedIP struct {
	SubnetID  string `json:"subnet_id"`
	IPAddress string `json:"ip_address"`
}

type portResponse struct {
	Port portBody `json:"port"`
}

func describePort(port domain.Port) portBody {
	fixedIPs := []portFixedIP{}
	if port.FixedIP.IsValid() {
		fixedIPs = append(fixedIPs, portFixedIP{SubnetID: port.SubnetID, IPAddress: port.FixedIP.String()})
	}
	return portBody{
		ID:           port.ID,
		Name:         port.Name,
		NetworkID:    port.NetworkID,
		ProjectID:    port.ProjectID,
		TenantID:     port.ProjectID,
		MACAddress:   port.MACAddress,
		AdminStateUp: !port.AdminDown,
		Status:       "DOWN",
		DeviceID:     port.DeviceID,
		DeviceOwner:  port.DeviceOwner,
		FixedIPs:     fixedIPs,
	}
}

func (h handler) createPort(w http.ResponseWriter, r *http.Request, projectID string) {
	data, ok := readCreateBody(w, r, "port")
	if !ok {
		return
	}
	params, err := parseCreatePort(data)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BadRequest", err.Error())
		return
	}
	if !params.matches(projectID) {
		writeError(w, http.StatusBadRequest, "BadRequest", "Port ownership must match the authenticated project.")
		return
	}

	port, err := h.service.CreatePort(projectID, params.networkID, params.name, !params.adminStateUp, params.subnetID)
	switch {
	case errors.Is(err, app.ErrNetworkNotFound):
		writeNetworkNotFound(w, params.networkID)
	case errors.Is(err, app.ErrPortSubnetRequired), errors.Is(err, app.ErrPortInvalidSubnet):
		writeError(w, http.StatusBadRequest, "InvalidInput", err.Error())
	case errors.Is(err, app.ErrPortIPExhausted):
		writeError(w, http.StatusConflict, "IpAddressGenerationFailure", "The subnet has no available addresses.")
	case err != nil:
		writeError(w, http.StatusInternalServerError, "HTTPInternalServerError", "Unable to create the port.")
	default:
		writeJSON(w, http.StatusCreated, portResponse{Port: describePort(port)})
	}
}

func (h handler) listPorts(w http.ResponseWriter, r *http.Request, projectID string) {
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BadRequest", "The port list query is malformed.")
		return
	}
	networkIDs := query["network_id"]
	if len(query) != 0 && (len(query) != 1 || networkIDs == nil) {
		writeError(w, http.StatusBadRequest, "BadRequest", "Port lists accept only network_id query parameters.")
		return
	}

	ports := h.service.ListPorts(projectID, networkIDs)
	response := struct {
		Ports []portBody `json:"ports"`
	}{Ports: make([]portBody, len(ports))}
	for i, port := range ports {
		response.Ports[i] = describePort(port)
	}
	writeJSON(w, http.StatusOK, response)
}

func (h handler) showPort(w http.ResponseWriter, r *http.Request, projectID string) {
	portID := chi.URLParam(r, "portID")
	port, err := h.service.GetPort(projectID, portID)
	if err != nil {
		writePortError(w, portID, err)
		return
	}
	writeJSON(w, http.StatusOK, portResponse{Port: describePort(port)})
}

func (h handler) deletePort(w http.ResponseWriter, r *http.Request, projectID string) {
	if !requireEmptyDeleteBody(w, r) {
		return
	}
	portID := chi.URLParam(r, "portID")
	if err := h.service.DeletePort(projectID, portID); err != nil {
		writePortError(w, portID, err)
		return
	}
	writeNoContent(w)
}

func writePortError(w http.ResponseWriter, portID string, err error) {
	if errors.Is(err, app.ErrPortNotFound) {
		writeError(w, http.StatusNotFound, "PortNotFound", fmt.Sprintf("Port %s could not be found.", portID))
		return
	}
	if errors.Is(err, app.ErrServicePortInUse) {
		writeError(w, http.StatusConflict, "ServicePortInUse", "Remove the router interface before deleting its port.")
		return
	}
	writeError(w, http.StatusInternalServerError, "HTTPInternalServerError", "Unable to process the port request.")
}
