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

func changeRouterInterface(change func(projectID, routerID, subnetID string) (domain.Port, error)) projectHandler {
	return func(w http.ResponseWriter, r *http.Request, projectID string) {
		data, ok := readCreateBody(w, r, "router interface")
		if !ok {
			return
		}
		fields, err := readUniqueObject(data)
		var subnetID string
		if err != nil || len(fields) != 1 || !decodeCreateField(fields["subnet_id"], &subnetID) ||
			!resourceUUIDPattern.MatchString(subnetID) {
			writeError(w, http.StatusBadRequest, "BadRequest", "A router interface request requires exactly one subnet UUID.")
			return
		}
		subnetID = strings.ToLower(subnetID)
		routerID := chi.URLParam(r, "routerID")
		port, err := change(projectID, routerID, subnetID)
		if err != nil {
			writeRouterInterfaceError(w, routerID, subnetID, err)
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

func writeRouterInterfaceError(w http.ResponseWriter, routerID, subnetID string, err error) {
	switch {
	case errors.Is(err, app.ErrRouterNotFound):
		writeRouterError(w, routerID, err)
	case errors.Is(err, app.ErrSubnetNotFound):
		writeSubnetError(w, subnetID, err)
	case errors.Is(err, app.ErrRouterInterfaceNotFound):
		writeError(w, http.StatusNotFound, "RouterInterfaceNotFoundForSubnet",
			fmt.Sprintf("Router %s has no interface on subnet %s.", routerID, subnetID))
	case errors.Is(err, app.ErrRouterSubnetAttached), errors.Is(err, app.ErrRouterSubnetOverlap):
		writeError(w, http.StatusBadRequest, "BadRequest", err.Error())
	case errors.Is(err, app.ErrRouterGatewayInUse):
		writeError(w, http.StatusConflict, "IpAddressInUse", "The subnet gateway IP address is already in use.")
	default:
		writeError(w, http.StatusInternalServerError, "HTTPInternalServerError", "Unable to process the router interface request.")
	}
}
