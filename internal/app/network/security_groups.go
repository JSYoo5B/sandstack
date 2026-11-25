package network

import (
	"errors"
	"slices"

	domain "github.com/JSYoo5B/sandstack/internal/domain/network"
)

const defaultSecurityGroupID = "9e19d5b5-73f3-41c2-bcd5-d7e6c83d6d2d"

var ErrSecurityGroupNotFound = errors.New("security group not found")

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
	if projectID != s.seed.ProjectID {
		return []domain.SecurityGroup{}
	}
	group := defaultSecurityGroup(projectID)
	if !filter.matches(group) {
		return []domain.SecurityGroup{}
	}
	return []domain.SecurityGroup{group}
}

func (s *Service) GetSecurityGroup(projectID, id string) (domain.SecurityGroup, error) {
	if projectID != s.seed.ProjectID || id != defaultSecurityGroupID {
		return domain.SecurityGroup{}, ErrSecurityGroupNotFound
	}
	return defaultSecurityGroup(projectID), nil
}

func defaultSecurityGroup(projectID string) domain.SecurityGroup {
	return domain.SecurityGroup{
		ID:          defaultSecurityGroupID,
		Name:        "default",
		Description: "Default security group",
		ProjectID:   projectID,
		Stateful:    true,
		Rules: []domain.SecurityGroupRule{
			{ID: "2af0ffd1-75e3-4b76-9c3c-49d7ed7dc6a3", Direction: "egress", EtherType: "IPv4"},
			{ID: "4c1e96fb-837f-4494-a3be-96ac09b9291e", Direction: "egress", EtherType: "IPv6"},
			{
				ID: "33aa02ab-8045-4e87-a724-491be582c6fe", Direction: "ingress",
				EtherType: "IPv4", RemoteGroupID: defaultSecurityGroupID,
			},
			{
				ID: "4025f933-de76-4940-aeaf-70248b39f59c", Direction: "ingress",
				EtherType: "IPv6", RemoteGroupID: defaultSecurityGroupID,
			},
		},
	}
}
