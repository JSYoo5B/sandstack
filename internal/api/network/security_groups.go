package network

import (
	"errors"
	"net/http"
	"net/url"

	"github.com/go-chi/chi/v5"

	app "github.com/JSYoo5B/sandstack/internal/app/network"
	domain "github.com/JSYoo5B/sandstack/internal/domain/network"
)

type securityGroupBody struct {
	ID          string                  `json:"id"`
	Name        string                  `json:"name"`
	Description string                  `json:"description"`
	ProjectID   string                  `json:"project_id"`
	TenantID    string                  `json:"tenant_id"`
	Stateful    bool                    `json:"stateful"`
	Shared      bool                    `json:"shared"`
	Rules       []securityGroupRuleBody `json:"security_group_rules"`
}

type securityGroupRuleBody struct {
	ID                   string  `json:"id"`
	Description          string  `json:"description"`
	SecurityGroupID      string  `json:"security_group_id"`
	ProjectID            string  `json:"project_id"`
	TenantID             string  `json:"tenant_id"`
	Direction            string  `json:"direction"`
	EtherType            string  `json:"ethertype"`
	Protocol             *string `json:"protocol"`
	PortRangeMin         *int    `json:"port_range_min"`
	PortRangeMax         *int    `json:"port_range_max"`
	RemoteGroupID        *string `json:"remote_group_id"`
	RemoteIPPrefix       *string `json:"remote_ip_prefix"`
	RemoteAddressGroupID *string `json:"remote_address_group_id"`
	NormalizedCIDR       *string `json:"normalized_cidr"`
	BelongsToDefault     *bool   `json:"belongs_to_default_sg,omitempty"`
}

type securityGroupResponse struct {
	Group securityGroupBody `json:"security_group"`
}

func (h handler) listSecurityGroups(w http.ResponseWriter, r *http.Request, projectID string) {
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BadRequest", "The security group list query is malformed.")
		return
	}
	for field := range query {
		switch field {
		case "id", "name", "tenant_id", "project_id":
		default:
			writeError(w, http.StatusBadRequest, "BadRequest", "The security group list query contains an unsupported filter.")
			return
		}
	}
	tenantIDs := query["tenant_id"]
	if query.Has("project_id") {
		tenantIDs = nil
	}
	groups := h.service.ListSecurityGroups(projectID, app.SecurityGroupFilter{
		IDs: query["id"], Names: query["name"], ProjectIDs: query["project_id"], TenantIDs: tenantIDs,
	})
	items := make([]securityGroupBody, len(groups))
	for i, group := range groups {
		items[i] = describeSecurityGroup(group)
	}
	writeJSON(w, http.StatusOK, struct {
		Groups []securityGroupBody `json:"security_groups"`
	}{Groups: items})
}

func (h handler) showSecurityGroup(w http.ResponseWriter, r *http.Request, projectID string) {
	group, err := h.service.GetSecurityGroup(projectID, chi.URLParam(r, "groupID"))
	switch {
	case errors.Is(err, app.ErrSecurityGroupNotFound):
		writeError(w, http.StatusNotFound, "SecurityGroupNotFound", "The security group could not be found.")
	case err != nil:
		writeError(w, http.StatusInternalServerError, "NeutronError", "Unable to read the security group.")
	default:
		writeJSON(w, http.StatusOK, securityGroupResponse{Group: describeSecurityGroup(group)})
	}
}

func describeSecurityGroup(group domain.SecurityGroup) securityGroupBody {
	rules := make([]securityGroupRuleBody, len(group.Rules))
	for i, rule := range group.Rules {
		rules[i] = describeSecurityGroupRule(rule, group.ID, group.ProjectID)
	}
	return securityGroupBody{
		ID:          group.ID,
		Name:        group.Name,
		Description: group.Description,
		ProjectID:   group.ProjectID,
		TenantID:    group.ProjectID,
		Stateful:    group.Stateful,
		Rules:       rules,
	}
}

func describeSecurityGroupRule(rule domain.SecurityGroupRule, groupID, projectID string) securityGroupRuleBody {
	body := securityGroupRuleBody{
		ID:              rule.ID,
		SecurityGroupID: groupID,
		ProjectID:       projectID,
		TenantID:        projectID,
		Direction:       rule.Direction,
		EtherType:       rule.EtherType,
		Description:     rule.Description,
	}
	if rule.RemoteGroupID != "" {
		body.RemoteGroupID = &rule.RemoteGroupID
	}
	if rule.Protocol != "" {
		body.Protocol = &rule.Protocol
	}
	body.PortRangeMin = rule.PortRangeMin
	body.PortRangeMax = rule.PortRangeMax
	if rule.RemoteIPPrefix.IsValid() {
		prefix := rule.RemoteIPPrefix.String()
		normalized := rule.RemoteIPPrefix.Masked().String()
		body.RemoteIPPrefix = &prefix
		body.NormalizedCIDR = &normalized
	}
	return body
}
