package network

import (
	"errors"
	"slices"
	"strings"

	domain "github.com/JSYoo5B/sandstack/internal/domain/network"
)

var ErrSecurityGroupDefaultUpdate = errors.New("the default security group name cannot be changed")

type SecurityGroupUpdate struct {
	Name        *string
	Description *string
	Stateful    *bool
}

func (s *Service) UpdateSecurityGroup(projectID, groupID string, changes SecurityGroupUpdate) (domain.SecurityGroup, error) {
	if changes.Name != nil && strings.EqualFold(*changes.Name, defaultSecurityGroupName) {
		return domain.SecurityGroup{}, ErrSecurityGroupDefaultName
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	group, found := s.securityGroups[groupID]
	if !found || group.ProjectID != projectID {
		return domain.SecurityGroup{}, ErrSecurityGroupNotFound
	}
	if changes.Stateful != nil && *changes.Stateful != group.Stateful {
		for _, port := range s.ports {
			if slices.Contains(port.SecurityGroups, groupID) {
				return domain.SecurityGroup{}, ErrSecurityGroupInUse
			}
		}
	}
	if changes.Name != nil && group.Name == defaultSecurityGroupName {
		return domain.SecurityGroup{}, ErrSecurityGroupDefaultUpdate
	}
	if changes.Name != nil {
		group.Name = *changes.Name
	}
	if changes.Description != nil {
		group.Description = *changes.Description
	}
	if changes.Stateful != nil {
		group.Stateful = *changes.Stateful
	}
	s.securityGroups[groupID] = group
	return cloneSecurityGroup(group), nil
}
