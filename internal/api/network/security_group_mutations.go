package network

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	app "github.com/JSYoo5B/sandstack/internal/app/network"
)

func (h handler) createSecurityGroup(w http.ResponseWriter, r *http.Request, projectID string) {
	data, ok := readCreateBody(w, r, "security group")
	if !ok {
		return
	}
	params, err := parseCreateSecurityGroup(data)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BadRequest", err.Error())
		return
	}
	if !params.matches(projectID) {
		writeError(w, http.StatusBadRequest, "BadRequest", "Security group ownership must match the authenticated project.")
		return
	}
	group, err := h.service.CreateSecurityGroup(projectID, params.name, params.description, params.stateful)
	switch {
	case errors.Is(err, app.ErrSecurityGroupDefaultName):
		writeError(w, http.StatusConflict, "SecurityGroupDefaultAlreadyExists", "The default security group already exists.")
	case err != nil:
		writeError(w, http.StatusInternalServerError, "NeutronError", "Unable to create the security group.")
	default:
		writeJSON(w, http.StatusCreated, securityGroupResponse{Group: describeSecurityGroup(group)})
	}
}

func (h handler) deleteSecurityGroup(w http.ResponseWriter, r *http.Request, projectID string) {
	err := h.service.DeleteSecurityGroup(projectID, chi.URLParam(r, "groupID"))
	switch {
	case errors.Is(err, app.ErrSecurityGroupNotFound):
		writeError(w, http.StatusNotFound, "SecurityGroupNotFound", "The security group could not be found.")
	case errors.Is(err, app.ErrSecurityGroupDefaultDelete):
		writeError(w, http.StatusConflict, "SecurityGroupCannotRemoveDefault", "Insufficient rights for removing default security group.")
	case errors.Is(err, app.ErrSecurityGroupInUse):
		writeError(w, http.StatusConflict, "SecurityGroupInUse", "The security group is bound to a port.")
	case err != nil:
		writeError(w, http.StatusInternalServerError, "NeutronError", "Unable to delete the security group.")
	default:
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusNoContent)
	}
}
