package network

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"

	app "github.com/JSYoo5B/sandstack/internal/app/network"
)

type securityGroupRuleResponse struct {
	Rule securityGroupRuleBody `json:"security_group_rule"`
}

func (h handler) listSecurityGroupRules(w http.ResponseWriter, r *http.Request, projectID string) {
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BadRequest", "The security group rule list query is malformed.")
		return
	}
	for field := range query {
		switch field {
		case "id", "security_group_id", "direction", "ethertype", "project_id", "tenant_id":
		default:
			writeError(w, http.StatusBadRequest, "BadRequest", "The security group rule list query contains an unsupported filter.")
			return
		}
	}
	var etherTypes []string
	for _, value := range query["ethertype"] {
		etherType := canonicalRuleEtherType(value)
		if etherType == "" {
			writeError(w, http.StatusBadRequest, "SecurityGroupRuleInvalidEtherType", "The rule ethertype must be IPv4 or IPv6.")
			return
		}
		etherTypes = append(etherTypes, etherType)
	}
	rules := h.service.ListSecurityGroupRules(projectID, app.SecurityGroupRuleFilter{
		IDs: query["id"], GroupIDs: query["security_group_id"], Directions: query["direction"],
		EtherTypes: etherTypes, ProjectIDs: query["project_id"], TenantIDs: query["tenant_id"],
	})
	items := make([]securityGroupRuleBody, len(rules))
	for i, detail := range rules {
		items[i] = describeStandaloneSecurityGroupRule(detail)
	}
	writeJSON(w, http.StatusOK, struct {
		Rules []securityGroupRuleBody `json:"security_group_rules"`
	}{Rules: items})
}

func canonicalRuleEtherType(value string) string {
	switch strings.ToLower(value) {
	case "ipv4":
		return "IPv4"
	case "ipv6":
		return "IPv6"
	default:
		return ""
	}
}

func (h handler) showSecurityGroupRule(w http.ResponseWriter, r *http.Request, projectID string) {
	detail, err := h.service.GetSecurityGroupRule(projectID, chi.URLParam(r, "ruleID"))
	switch {
	case errors.Is(err, app.ErrSecurityGroupRuleNotFound):
		writeError(w, http.StatusNotFound, "SecurityGroupRuleNotFound", "The security group rule could not be found.")
	case err != nil:
		writeError(w, http.StatusInternalServerError, "NeutronError", "Unable to read the security group rule.")
	default:
		writeJSON(w, http.StatusOK, securityGroupRuleResponse{Rule: describeStandaloneSecurityGroupRule(detail)})
	}
}

func describeStandaloneSecurityGroupRule(detail app.SecurityGroupRuleDetail) securityGroupRuleBody {
	body := describeSecurityGroupRule(detail.Rule, detail.GroupID, detail.ProjectID)
	body.BelongsToDefault = &detail.BelongsToDefault
	return body
}
