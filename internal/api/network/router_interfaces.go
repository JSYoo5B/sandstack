package network

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	app "github.com/JSYoo5B/sandstack/internal/app/network"
	domain "github.com/JSYoo5B/sandstack/internal/domain/network"
)

type routerInterfaceBody struct {
	ID        string   `json:"id"`
	PortID    string   `json:"port_id"`
	NetworkID string   `json:"network_id"`
	SubnetID  string   `json:"subnet_id"`
	SubnetIDs []string `json:"subnet_ids"`
	ProjectID string   `json:"project_id"`
	TenantID  string   `json:"tenant_id"`
	Tags      []string `json:"tags"`
}

type routerInterfaceAction int

const (
	addRouterInterface routerInterfaceAction = iota
	removeRouterInterface
)

func (h handler) changeRouterInterface(action routerInterfaceAction) projectHandler {
	return func(w http.ResponseWriter, r *http.Request, projectID string) {
		data, ok := readCreateBody(w, r, "router interface")
		if !ok {
			return
		}
		portID, subnetID, err := parseRouterInterface(data)
		if err != nil {
			writeError(w, http.StatusBadRequest, "BadRequest", err.Error())
			return
		}
		if action == addRouterInterface && portID != "" && subnetID != "" {
			writeError(w, http.StatusBadRequest, "BadRequest", "Adding a router interface requires exactly one resource UUID.")
			return
		}
		routerID := chi.URLParam(r, "routerID")
		var port domain.Port
		switch {
		case action == addRouterInterface && portID != "":
			port, err = h.service.AddRouterInterfaceByPort(projectID, routerID, portID)
		case action == addRouterInterface:
			port, err = h.service.AddRouterInterface(projectID, routerID, subnetID)
		case portID != "":
			port, err = h.service.RemoveRouterInterfaceByPort(projectID, routerID, portID, subnetID)
		default:
			port, err = h.service.RemoveRouterInterface(projectID, routerID, subnetID)
		}
		if err != nil {
			writeRouterInterfaceError(w, routerID, portID, subnetID, err)
			return
		}
		writeJSON(w, http.StatusOK, routerInterfaceBody{
			ID:        port.DeviceID,
			PortID:    port.ID,
			NetworkID: port.NetworkID,
			SubnetID:  port.SubnetID,
			SubnetIDs: []string{port.SubnetID},
			ProjectID: port.ProjectID,
			TenantID:  port.ProjectID,
			Tags:      []string{},
		})
	}
}

var errInvalidInterfaceRequest = errors.New("a router interface request requires a valid port_id or subnet_id UUID")

func parseRouterInterface(data []byte) (portID, subnetID string, err error) {
	fields, err := readUniqueObject(data)
	if err != nil || len(fields) == 0 {
		return "", "", errInvalidInterfaceRequest
	}
	for field, value := range fields {
		var id string
		if !decodeCreateField(value, &id) || !resourceUUIDPattern.MatchString(id) {
			return "", "", errInvalidInterfaceRequest
		}
		switch field {
		case "port_id":
			portID = strings.ToLower(id)
		case "subnet_id":
			subnetID = strings.ToLower(id)
		default:
			return "", "", errInvalidInterfaceRequest
		}
	}
	return portID, subnetID, nil
}

func writeRouterInterfaceError(w http.ResponseWriter, routerID, portID, subnetID string, err error) {
	switch {
	case errors.Is(err, app.ErrRouterNotFound):
		writeRouterError(w, routerID, err)
	case errors.Is(err, app.ErrSubnetNotFound):
		writeSubnetError(w, subnetID, err)
	case errors.Is(err, app.ErrPortNotFound):
		writePortError(w, portID, err)
	case errors.Is(err, app.ErrRouterInterfacePortNotFound):
		writeError(w, http.StatusNotFound, "RouterInterfaceNotFound",
			fmt.Sprintf("Router %s has no interface with port %s.", routerID, portID))
	case errors.Is(err, app.ErrRouterInterfaceNotFound):
		writeError(w, http.StatusNotFound, "RouterInterfaceNotFoundForSubnet",
			fmt.Sprintf("Router %s has no interface on subnet %s.", routerID, subnetID))
	case errors.Is(err, app.ErrRouterSubnetAttached), errors.Is(err, app.ErrRouterSubnetOverlap),
		errors.Is(err, app.ErrRouterPortNoFixedIP):
		writeError(w, http.StatusBadRequest, "BadRequest", err.Error())
	case errors.Is(err, app.ErrRouterInterfaceSubnetMismatch):
		writeError(w, http.StatusBadRequest, "SubnetMismatchForPort",
			fmt.Sprintf("Port %s does not belong to subnet %s.", portID, subnetID))
	case errors.Is(err, app.ErrPortInUse):
		writeError(w, http.StatusConflict, "PortInUse", fmt.Sprintf("Port %s is already in use.", portID))
	case errors.Is(err, app.ErrRouterGatewayInUse):
		writeError(w, http.StatusConflict, "IpAddressInUse", "The subnet gateway IP address is already in use.")
	default:
		writeError(w, http.StatusInternalServerError, "HTTPInternalServerError", "Unable to process the router interface request.")
	}
}
