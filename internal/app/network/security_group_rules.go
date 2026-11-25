package network

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	domain "github.com/JSYoo5B/sandstack/internal/domain/network"
)

var (
	ErrSecurityGroupRuleNotFound  = errors.New("security group rule not found")
	ErrSecurityGroupRuleForbidden = errors.New("security group rule creation is forbidden")
	ErrSecurityGroupRuleExists    = errors.New("security group rule already exists")
)

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
	s.mu.RLock()
	for _, group := range s.securityGroups {
		if group.ProjectID != projectID {
			continue
		}
		for _, rule := range group.Rules {
			detail := describeSecurityGroupRule(group, rule)
			if filter.matches(detail) {
				rules = append(rules, detail)
			}
		}
	}
	s.mu.RUnlock()
	slices.SortFunc(rules, func(a, b SecurityGroupRuleDetail) int { return strings.Compare(a.Rule.ID, b.Rule.ID) })
	return rules
}

func (s *Service) GetSecurityGroupRule(projectID, ruleID string) (SecurityGroupRuleDetail, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	group, index := s.findOwnedSecurityGroupRule(projectID, ruleID)
	if index < 0 {
		return SecurityGroupRuleDetail{}, ErrSecurityGroupRuleNotFound
	}
	return describeSecurityGroupRule(group, group.Rules[index]), nil
}

func (s *Service) CreateSecurityGroupRule(projectID, groupID string, rule domain.SecurityGroupRule) (SecurityGroupRuleDetail, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	group, found := s.securityGroups[groupID]
	if !found {
		return SecurityGroupRuleDetail{}, ErrSecurityGroupNotFound
	}
	if group.ProjectID != projectID {
		return SecurityGroupRuleDetail{}, ErrSecurityGroupRuleForbidden
	}
	signature := securityGroupRuleSignature(rule)
	for _, existing := range group.Rules {
		if securityGroupRuleSignature(existing) == signature {
			return SecurityGroupRuleDetail{}, fmt.Errorf("%w: %s", ErrSecurityGroupRuleExists, existing.ID)
		}
	}
	id, err := s.allocateID(nil)
	if err != nil {
		return SecurityGroupRuleDetail{}, err
	}
	rule.ID = id
	group.Rules = append(group.Rules, rule)
	s.securityGroups[group.ID] = group
	s.securityGroupRuleGroups[rule.ID] = group.ID
	return describeSecurityGroupRule(group, rule), nil
}

func (s *Service) DeleteSecurityGroupRule(projectID, ruleID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	group, index := s.findOwnedSecurityGroupRule(projectID, ruleID)
	if index < 0 {
		return ErrSecurityGroupRuleNotFound
	}
	group.Rules = slices.Delete(group.Rules, index, index+1)
	s.securityGroups[group.ID] = group
	delete(s.securityGroupRuleGroups, ruleID)
	return nil
}

func (s *Service) findOwnedSecurityGroupRule(projectID, ruleID string) (domain.SecurityGroup, int) {
	groupID := s.securityGroupRuleGroups[ruleID]
	group, found := s.securityGroups[groupID]
	if !found || group.ProjectID != projectID {
		return domain.SecurityGroup{}, -1
	}
	return group, slices.IndexFunc(group.Rules, func(rule domain.SecurityGroupRule) bool {
		return rule.ID == ruleID
	})
}

func securityGroupRuleSignature(rule domain.SecurityGroupRule) domain.SecurityGroupRule {
	rule.ID = ""
	rule.Description = ""
	return rule
}

func describeSecurityGroupRule(group domain.SecurityGroup, rule domain.SecurityGroupRule) SecurityGroupRuleDetail {
	return SecurityGroupRuleDetail{
		Rule: rule, GroupID: group.ID, ProjectID: group.ProjectID, BelongsToDefault: group.ID == defaultSecurityGroupID,
	}
}
