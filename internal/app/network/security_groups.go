package network

import (
	"errors"
	"slices"
	"strings"

	domain "github.com/JSYoo5B/sandstack/internal/domain/network"
)

const (
	defaultSecurityGroupName = "default"
	defaultSecurityGroupID   = "9e19d5b5-73f3-41c2-bcd5-d7e6c83d6d2d"
	defaultIPv4EgressID      = "2af0ffd1-75e3-4b76-9c3c-49d7ed7dc6a3"
	defaultIPv6EgressID      = "4c1e96fb-837f-4494-a3be-96ac09b9291e"
	defaultIPv4IngressID     = "33aa02ab-8045-4e87-a724-491be582c6fe"
	defaultIPv6IngressID     = "4025f933-de76-4940-aeaf-70248b39f59c"
)

var (
	ErrSecurityGroupNotFound      = errors.New("security group not found")
	ErrSecurityGroupInUse         = errors.New("security group is bound to a port")
	ErrSecurityGroupConflict      = domain.ErrSecurityGroupConflict
	ErrSecurityGroupDefaultName   = errors.New("the default security group name is reserved")
	ErrSecurityGroupDefaultDelete = errors.New("the default security group cannot be deleted")
)

type SecurityGroupNotFoundError struct{ ID string }

func (e *SecurityGroupNotFoundError) Error() string {
	return ErrSecurityGroupNotFound.Error() + ": " + e.ID
}

func (e *SecurityGroupNotFoundError) Unwrap() error { return ErrSecurityGroupNotFound }

// portSecurityGroups resolves a port's owned groups while the write lock is held.
func (s *Service) portSecurityGroups(projectID string, requested *[]string) ([]string, error) {
	if requested == nil {
		if projectID == s.seed.ProjectID {
			return []string{defaultSecurityGroupID}, nil
		}
		return nil, nil
	}
	var hasStateful, hasStateless bool
	for _, id := range *requested {
		group, found := s.securityGroups[id]
		if !found || group.ProjectID != projectID {
			return nil, &SecurityGroupNotFoundError{ID: id}
		}
		hasStateful = hasStateful || group.Stateful
		hasStateless = hasStateless || !group.Stateful
	}
	if hasStateful && hasStateless {
		return nil, ErrSecurityGroupConflict
	}
	return slices.Clone(*requested), nil
}

type SecurityGroupFilter struct {
	IDs        []string
	Names      []string
	ProjectIDs []string
	TenantIDs  []string
}

func (f SecurityGroupFilter) matches(group domain.SecurityGroup) bool {
	return (len(f.IDs) == 0 || slices.Contains(f.IDs, group.ID)) &&
		(len(f.Names) == 0 || slices.Contains(f.Names, group.Name)) &&
		(len(f.ProjectIDs) == 0 || slices.Contains(f.ProjectIDs, group.ProjectID)) &&
		(len(f.TenantIDs) == 0 || slices.Contains(f.TenantIDs, group.ProjectID))
}

func (s *Service) ListSecurityGroups(projectID string, filter SecurityGroupFilter) []domain.SecurityGroup {
	groups := []domain.SecurityGroup{}
	s.mu.RLock()
	for _, group := range s.securityGroups {
		if group.ProjectID == projectID && filter.matches(group) {
			groups = append(groups, cloneSecurityGroup(group))
		}
	}
	s.mu.RUnlock()
	slices.SortFunc(groups, func(a, b domain.SecurityGroup) int { return strings.Compare(a.ID, b.ID) })
	return groups
}

func (s *Service) GetSecurityGroup(projectID, id string) (domain.SecurityGroup, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	group, found := s.securityGroups[id]
	if !found || group.ProjectID != projectID {
		return domain.SecurityGroup{}, ErrSecurityGroupNotFound
	}
	return cloneSecurityGroup(group), nil
}

func (s *Service) CreateSecurityGroup(projectID, name, description string, stateful bool) (domain.SecurityGroup, error) {
	if strings.EqualFold(name, defaultSecurityGroupName) {
		return domain.SecurityGroup{}, ErrSecurityGroupDefaultName
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	// Reserve the group ID and both rule IDs before publishing any resource.
	var ids [3]string
	for i := range ids {
		id, err := s.allocateID(func(id string) bool { return !slices.Contains(ids[:i], id) })
		if err != nil {
			return domain.SecurityGroup{}, err
		}
		ids[i] = id
	}
	group := domain.SecurityGroup{
		ID:          ids[0],
		Name:        name,
		Description: description,
		ProjectID:   projectID,
		Stateful:    stateful,
		Rules:       egressSecurityGroupRules(ids[1], ids[2]),
	}
	s.securityGroups[group.ID] = group
	for _, rule := range group.Rules {
		s.securityGroupRuleGroups[rule.ID] = group.ID
	}
	return cloneSecurityGroup(group), nil
}

func (s *Service) DeleteSecurityGroup(projectID, groupID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	group, found := s.securityGroups[groupID]
	if !found || group.ProjectID != projectID {
		return ErrSecurityGroupNotFound
	}
	if groupID == defaultSecurityGroupID {
		return ErrSecurityGroupDefaultDelete
	}
	for _, port := range s.ports {
		if slices.Contains(port.SecurityGroups, groupID) {
			return ErrSecurityGroupInUse
		}
	}
	delete(s.securityGroups, groupID)
	for _, rule := range group.Rules {
		delete(s.securityGroupRuleGroups, rule.ID)
	}
	for id, other := range s.securityGroups {
		other.Rules = slices.DeleteFunc(other.Rules, func(rule domain.SecurityGroupRule) bool {
			if rule.RemoteGroupID != groupID {
				return false
			}
			delete(s.securityGroupRuleGroups, rule.ID)
			return true
		})
		s.securityGroups[id] = other
	}
	return nil
}

func (s *Service) resetSecurityGroups() {
	group := defaultSecurityGroup(s.seed.ProjectID)
	s.securityGroups = map[string]domain.SecurityGroup{group.ID: group}
	s.securityGroupRuleGroups = make(map[string]string, len(group.Rules))
	for _, rule := range group.Rules {
		s.securityGroupRuleGroups[rule.ID] = group.ID
	}
}

func defaultSecurityGroup(projectID string) domain.SecurityGroup {
	return domain.SecurityGroup{
		ID:          defaultSecurityGroupID,
		Name:        defaultSecurityGroupName,
		Description: "Default security group",
		ProjectID:   projectID,
		Stateful:    true,
		Rules: append(egressSecurityGroupRules(defaultIPv4EgressID, defaultIPv6EgressID),
			domain.SecurityGroupRule{
				ID: defaultIPv4IngressID, Direction: "ingress",
				EtherType: "IPv4", RemoteGroupID: defaultSecurityGroupID,
			},
			domain.SecurityGroupRule{
				ID: defaultIPv6IngressID, Direction: "ingress",
				EtherType: "IPv6", RemoteGroupID: defaultSecurityGroupID,
			},
		),
	}
}

func egressSecurityGroupRules(ipv4ID, ipv6ID string) []domain.SecurityGroupRule {
	return []domain.SecurityGroupRule{
		{ID: ipv4ID, Direction: "egress", EtherType: "IPv4"},
		{ID: ipv6ID, Direction: "egress", EtherType: "IPv6"},
	}
}

func cloneSecurityGroup(group domain.SecurityGroup) domain.SecurityGroup {
	group.Rules = slices.Clone(group.Rules)
	return group
}

func isDefaultSecurityGroupID(id string) bool {
	switch id {
	case defaultSecurityGroupID, defaultIPv4EgressID, defaultIPv6EgressID, defaultIPv4IngressID, defaultIPv6IngressID:
		return true
	default:
		return false
	}
}
