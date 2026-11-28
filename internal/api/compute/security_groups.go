package compute

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	appcompute "github.com/JSYoo5B/sandstack/internal/app/compute"
)

type serverSecurityGroupBody struct {
	ID          string                        `json:"id"`
	Name        string                        `json:"name"`
	Description string                        `json:"description"`
	TenantID    string                        `json:"tenant_id"`
	Rules       []serverSecurityGroupRuleBody `json:"rules"`
}

type serverSecurityGroupRuleBody struct {
	ID            string            `json:"id"`
	ParentGroupID string            `json:"parent_group_id"`
	IPProtocol    *string           `json:"ip_protocol"`
	FromPort      *int              `json:"from_port"`
	ToPort        *int              `json:"to_port"`
	Group         map[string]string `json:"group"`
	IPRange       map[string]string `json:"ip_range"`
}

func (h handler) changeServerSecurityGroup(
	w http.ResponseWriter, r *http.Request, scope requestScope, action string, raw json.RawMessage,
) {
	name, err := parseSecurityGroupAction(action, raw)
	if err != nil {
		writeFault(w, http.StatusBadRequest, "badRequest", err.Error())
		return
	}

	serverID := chi.URLParam(r, "serverID")
	if action == "addSecurityGroup" {
		err = h.servers.AddSecurityGroup(scope.actionRequest(), serverID, name)
	} else {
		err = h.servers.RemoveSecurityGroup(scope.actionRequest(), serverID, name)
	}
	var groupErr *appcompute.SecurityGroupError
	switch {
	case errors.Is(err, appcompute.ErrServerNotFound):
		writeFault(w, http.StatusNotFound, "itemNotFound", "The server could not be found.")
	case errors.Is(err, appcompute.ErrSecurityGroupNotFound) && errors.As(err, &groupErr):
		writeFault(w, http.StatusNotFound, "itemNotFound", groupErr.Message)
	case errors.Is(err, appcompute.ErrSecurityGroupAmbiguous) && errors.As(err, &groupErr),
		errors.Is(err, appcompute.ErrSecurityGroupStateConflict) && errors.As(err, &groupErr):
		writeFault(w, http.StatusConflict, "conflictingRequest", groupErr.Message)
	case errors.As(err, &groupErr):
		writeFault(w, http.StatusBadRequest, "badRequest", groupErr.Message)
	case err != nil:
		writeFault(w, http.StatusInternalServerError, "computeFault", "Unable to change the server's security groups.")
	default:
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusAccepted)
	}
}

func parseSecurityGroupAction(action string, raw json.RawMessage) (string, error) {
	fields, err := readUniqueObject(raw)
	if err != nil {
		return "", errors.New("Invalid input for field/attribute " + action + ": expected an object.")
	}
	for key := range fields {
		if key != "name" {
			return "", errors.New("Additional properties are not allowed ('" + key + "' was unexpected).")
		}
	}
	value, ok := fields["name"]
	if !ok {
		return "", errors.New("'name' is a required property.")
	}
	var name string
	if json.Unmarshal(value, &name) != nil || value[0] != '"' || name == "" {
		return "", errors.New("Invalid input for field 'name'.")
	}
	return name, nil
}

func (h handler) listServerSecurityGroups(w http.ResponseWriter, r *http.Request, scope requestScope) {
	groups, err := h.servers.ListSecurityGroups(scope.projectID, chi.URLParam(r, "serverID"))
	switch {
	case errors.Is(err, appcompute.ErrServerNotFound):
		writeFault(w, http.StatusNotFound, "itemNotFound", "The server could not be found.")
		return
	case err != nil:
		writeFault(w, http.StatusInternalServerError, "computeFault", "Unable to list the server's security groups.")
		return
	}
	response := struct {
		SecurityGroups []serverSecurityGroupBody `json:"security_groups"`
	}{SecurityGroups: make([]serverSecurityGroupBody, len(groups))}
	for i, group := range groups {
		response.SecurityGroups[i] = describeServerSecurityGroup(group)
	}
	writeJSON(w, http.StatusOK, response)
}

func describeServerSecurityGroup(group appcompute.ServerSecurityGroup) serverSecurityGroupBody {
	body := serverSecurityGroupBody{
		ID:          group.ID,
		Name:        group.Name,
		Description: group.Description,
		TenantID:    group.ProjectID,
		Rules:       make([]serverSecurityGroupRuleBody, len(group.Rules)),
	}
	for i, rule := range group.Rules {
		ruleBody := serverSecurityGroupRuleBody{
			ID:            rule.ID,
			ParentGroupID: rule.ParentGroupID,
			IPProtocol:    rule.Protocol,
			FromPort:      rule.FromPort,
			ToPort:        rule.ToPort,
			Group:         map[string]string{},
			IPRange:       map[string]string{},
		}
		if rule.RemoteGroup != nil {
			ruleBody.Group["name"] = rule.RemoteGroup.Name
			ruleBody.Group["tenant_id"] = rule.RemoteGroup.ProjectID
		} else {
			ruleBody.IPRange["cidr"] = rule.CIDR
		}
		body.Rules[i] = ruleBody
	}
	return body
}
