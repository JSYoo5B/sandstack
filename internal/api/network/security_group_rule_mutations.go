package network

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	app "github.com/JSYoo5B/sandstack/internal/app/network"
)

func (h handler) createSecurityGroupRule(w http.ResponseWriter, r *http.Request, projectID string) {
	data, ok := readCreateBody(w, r, "security group rule")
	if !ok {
		return
	}
	params, fault := parseCreateSecurityGroupRule(data)
	if fault != nil {
		writeError(w, http.StatusBadRequest, fault.Type, fault.Message)
		return
	}
	if !params.matches(projectID) {
		writeError(w, http.StatusBadRequest, "BadRequest", "Security group rule ownership must match the authenticated project.")
		return
	}
	detail, err := h.service.CreateSecurityGroupRule(projectID, params.groupID, params.rule)
	switch {
	case errors.Is(err, app.ErrSecurityGroupNotFound):
		writeError(w, http.StatusNotFound, "SecurityGroupNotFound", "The security group could not be found.")
	case errors.Is(err, app.ErrSecurityGroupRuleForbidden):
		writeError(w, http.StatusForbidden, "PolicyNotAuthorized", "The security group is owned by another project.")
	case errors.Is(err, app.ErrSecurityGroupRuleExists):
		writeError(w, http.StatusConflict, "SecurityGroupRuleExists", err.Error())
	case err != nil:
		writeError(w, http.StatusInternalServerError, "NeutronError", "Unable to create the security group rule.")
	default:
		writeJSON(w, http.StatusCreated, securityGroupRuleResponse{Rule: describeStandaloneSecurityGroupRule(detail)})
	}
}

func (h handler) deleteSecurityGroupRule(w http.ResponseWriter, r *http.Request, projectID string) {
	err := h.service.DeleteSecurityGroupRule(projectID, chi.URLParam(r, "ruleID"))
	switch {
	case errors.Is(err, app.ErrSecurityGroupRuleNotFound):
		writeError(w, http.StatusNotFound, "SecurityGroupRuleNotFound", "The security group rule could not be found.")
	case err != nil:
		writeError(w, http.StatusInternalServerError, "NeutronError", "Unable to delete the security group rule.")
	default:
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusNoContent)
	}
}
