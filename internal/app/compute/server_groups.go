package compute

import (
	"errors"
	"slices"

	domain "github.com/JSYoo5B/sandstack/internal/domain/compute"
)

const (
	maxServerGroups       = 10
	maxServerGroupMembers = 10
)

var (
	ErrServerGroupNotFound     = errors.New("server group not found")
	ErrServerGroupQuota        = errors.New("Quota exceeded, too many server groups.")
	ErrServerGroupMemberQuota  = errors.New("Quota exceeded, too many servers in group")
	ErrServerGroupIDGeneration = errors.New("server group ID generation failed")
)

type ServerGroups struct {
	compute *Service
	newID   func() string
}

func NewServerGroups(compute *Service, newID func() string) *ServerGroups {
	return &ServerGroups{compute: compute, newID: newID}
}

func (g *ServerGroups) Create(request ActionRequest, name, policy string) (domain.ServerGroup, error) {
	g.compute.mu.Lock()
	defer g.compute.mu.Unlock()

	count := 0
	for _, group := range g.compute.groups {
		if group.ProjectID == request.ProjectID {
			count++
		}
	}
	if count >= maxServerGroups {
		return domain.ServerGroup{}, ErrServerGroupQuota
	}
	id, err := g.allocateID()
	if err != nil {
		return domain.ServerGroup{}, err
	}
	group := &domain.ServerGroup{
		ID: id, Name: name, ProjectID: request.ProjectID, UserID: request.UserID, Policy: policy,
	}
	g.compute.groups = append(g.compute.groups, group)
	return cloneServerGroup(group), nil
}

func (g *ServerGroups) allocateID() (string, error) {
	for range 8 {
		id := g.newID()
		if id != "" && !slices.ContainsFunc(g.compute.groups, func(group *domain.ServerGroup) bool {
			return group.ID == id
		}) {
			return id, nil
		}
	}
	return "", ErrServerGroupIDGeneration
}

func (g *ServerGroups) List(projectID string) []domain.ServerGroup {
	g.compute.mu.RLock()
	defer g.compute.mu.RUnlock()

	var groups []domain.ServerGroup
	for _, group := range g.compute.groups {
		if group.ProjectID == projectID {
			groups = append(groups, cloneServerGroup(group))
		}
	}
	return groups
}

func (g *ServerGroups) Get(projectID, groupID string) (domain.ServerGroup, error) {
	g.compute.mu.RLock()
	defer g.compute.mu.RUnlock()

	group, ok := g.compute.findGroup(projectID, groupID)
	if !ok {
		return domain.ServerGroup{}, ErrServerGroupNotFound
	}
	return cloneServerGroup(group), nil
}

func (g *ServerGroups) Delete(projectID, groupID string) error {
	g.compute.mu.Lock()
	defer g.compute.mu.Unlock()

	if _, ok := g.compute.findGroup(projectID, groupID); !ok {
		return ErrServerGroupNotFound
	}
	g.compute.groups = slices.DeleteFunc(g.compute.groups, func(group *domain.ServerGroup) bool {
		return group.ID == groupID
	})
	return nil
}

func (s *Service) findGroup(projectID, groupID string) (*domain.ServerGroup, bool) {
	for _, group := range s.groups {
		if group.ID == groupID && group.ProjectID == projectID {
			return group, true
		}
	}
	return nil, false
}

func (s *Servers) requestedGroup(projectID, groupID string) error {
	if groupID == "" {
		return nil
	}
	s.compute.mu.RLock()
	defer s.compute.mu.RUnlock()

	if _, ok := s.compute.findGroup(projectID, groupID); !ok {
		return createError(ErrServerGroupNotFound, "Instance group %s could not be found.", groupID)
	}
	return nil
}

func (s *Servers) checkGroupMemberQuota(request ActionRequest, groupID string) error {
	if groupID == "" {
		return nil
	}
	s.compute.mu.RLock()
	defer s.compute.mu.RUnlock()

	group, ok := s.compute.findGroup(request.ProjectID, groupID)
	if !ok {
		return createError(ErrServerGroupNotFound, "Instance group %s could not be found.", groupID)
	}
	count := 0
	for _, member := range group.Members {
		if record, ok := s.compute.servers[member]; ok && record.server.UserID == request.UserID {
			count++
		}
	}
	if count >= maxServerGroupMembers {
		return createError(ErrServerGroupMemberQuota, "Quota exceeded, too many servers in group")
	}
	return nil
}

func (s *Service) groupHostTaken(projectID, groupID, serverID string) bool {
	group, ok := s.findGroup(projectID, groupID)
	if !ok || group.Policy != domain.PolicyAntiAffinity {
		return false
	}
	for _, member := range group.Members {
		if record, ok := s.servers[member]; ok && member != serverID && record.server.Placed {
			return true
		}
	}
	return false
}

func (s *Service) memberGroupID(serverID string) (string, bool) {
	for _, group := range s.groups {
		if slices.Contains(group.Members, serverID) {
			return group.ID, true
		}
	}
	return "", false
}

func (s *Service) joinGroup(projectID, groupID, serverID string) {
	if group, ok := s.findGroup(projectID, groupID); ok {
		group.Members = append(group.Members, serverID)
	}
}

func (s *Service) leaveGroups(serverID string) {
	for _, group := range s.groups {
		group.Members = slices.DeleteFunc(group.Members, func(member string) bool { return member == serverID })
	}
}

func cloneServerGroup(group *domain.ServerGroup) domain.ServerGroup {
	clone := *group
	clone.Members = slices.Clone(group.Members)
	if clone.Members == nil {
		clone.Members = []string{}
	}
	return clone
}
