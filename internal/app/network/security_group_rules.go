package network

import (
	"errors"
	"slices"
	"strings"

	domain "github.com/JSYoo5B/sandstack/internal/domain/network"
)

var ErrSecurityGroupRuleNotFound = errors.New("security group rule not found")

type SecurityGroupRuleDetail struct {
	Rule             domain.SecurityGroupRule
	GroupID          string
	ProjectID        string
	BelongsToDefault bool
}

type SecurityGroupRuleFilter struct {
	IDs        []string
	GroupIDs   []string
	Directions []string
	EtherTypes []string
	ProjectIDs []string
	TenantIDs  []string
}

func (f SecurityGroupRuleFilter) matches(detail SecurityGroupRuleDetail) bool {
	return (len(f.IDs) == 0 || slices.Contains(f.IDs, detail.Rule.ID)) &&
		(len(f.GroupIDs) == 0 || slices.Contains(f.GroupIDs, detail.GroupID)) &&
		(len(f.Directions) == 0 || slices.Contains(f.Directions, detail.Rule.Direction)) &&
		(len(f.EtherTypes) == 0 || slices.Contains(f.EtherTypes, detail.Rule.EtherType)) &&
		(len(f.ProjectIDs) == 0 || slices.Contains(f.ProjectIDs, detail.ProjectID)) &&
		(len(f.TenantIDs) == 0 || slices.Contains(f.TenantIDs, detail.ProjectID))
}

func (s *Service) ListSecurityGroupRules(projectID string, filter SecurityGroupRuleFilter) []SecurityGroupRuleDetail {
	rules := []SecurityGroupRuleDetail{}
	appendRules := func(group domain.SecurityGroup) {
		for _, rule := range group.Rules {
			detail := describeSecurityGroupRule(group, rule)
			if filter.matches(detail) {
				rules = append(rules, detail)
			}
		}
	}
	if projectID == s.seed.ProjectID {
		appendRules(defaultSecurityGroup(projectID))
	}
	s.mu.RLock()
	for _, group := range s.securityGroups {
		if group.ProjectID == projectID {
			appendRules(group)
		}
	}
	s.mu.RUnlock()
	slices.SortFunc(rules, func(a, b SecurityGroupRuleDetail) int { return strings.Compare(a.Rule.ID, b.Rule.ID) })
	return rules
}

func (s *Service) GetSecurityGroupRule(projectID, ruleID string) (SecurityGroupRuleDetail, error) {
	var group domain.SecurityGroup
	if projectID == s.seed.ProjectID && isDefaultSecurityGroupID(ruleID) {
		group = defaultSecurityGroup(projectID)
	} else {
		s.mu.RLock()
		defer s.mu.RUnlock()
		groupID := s.securityGroupRuleGroups[ruleID]
		var found bool
		group, found = s.securityGroups[groupID]
		if !found || group.ProjectID != projectID {
			return SecurityGroupRuleDetail{}, ErrSecurityGroupRuleNotFound
		}
	}
	for _, rule := range group.Rules {
		if rule.ID == ruleID {
			return describeSecurityGroupRule(group, rule), nil
		}
	}
	return SecurityGroupRuleDetail{}, ErrSecurityGroupRuleNotFound
}

func describeSecurityGroupRule(group domain.SecurityGroup, rule domain.SecurityGroupRule) SecurityGroupRuleDetail {
	return SecurityGroupRuleDetail{
		Rule: rule, GroupID: group.ID, ProjectID: group.ProjectID, BelongsToDefault: group.ID == defaultSecurityGroupID,
	}
}
