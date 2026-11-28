package network

import (
	"errors"
	"slices"
)

const MaxResourceTags = 50

var (
	ErrTagResourceNotFound  = errors.New("tagged resource not found")
	ErrTagResourceForbidden = errors.New("tagged resource is not accessible to the project")
	ErrTagNotFound          = errors.New("tag not found")
	ErrTagLimitExceeded     = errors.New("the number of tags exceeds the per-resource limit")
)

type TagResource int

const (
	TagNetwork TagResource = iota
	TagSubnet
	TagPort
	TagRouter
	TagSecurityGroup
	TagFloatingIP
)

type TagFilter struct {
	All    []string
	Any    []string
	NotAll []string
	NotAny []string
}

func (f TagFilter) matches(tags []string) bool {
	has := func(tag string) bool { return slices.Contains(tags, tag) }
	all := func(wanted []string) bool {
		for _, tag := range wanted {
			if !has(tag) {
				return false
			}
		}
		return true
	}
	return (len(f.All) == 0 || all(f.All)) &&
		(len(f.Any) == 0 || slices.ContainsFunc(f.Any, has)) &&
		(len(f.NotAll) == 0 || !all(f.NotAll)) &&
		(len(f.NotAny) == 0 || !slices.ContainsFunc(f.NotAny, has))
}

type taggedResource struct {
	tags    []string
	visible bool
	owned   bool
	store   func([]string)
}

func (s *Service) taggedResource(projectID string, kind TagResource, id string) (taggedResource, bool) {
	switch kind {
	case TagNetwork:
		network, ok := s.networks[id]
		return taggedResource{
			tags:    network.Tags,
			visible: visibleNetwork(network, projectID),
			owned:   network.ProjectID == projectID,
			store:   func(tags []string) { network.Tags = tags; s.networks[id] = network },
		}, ok
	case TagSubnet:
		subnet, ok := s.subnets[id]
		return taggedResource{
			tags:    subnet.Tags,
			visible: ok && s.visibleSubnet(subnet, projectID),
			owned:   subnet.ProjectID == projectID,
			store:   func(tags []string) { subnet.Tags = tags; s.subnets[id] = subnet },
		}, ok
	case TagPort:
		port, ok := s.ports[id]
		return taggedResource{
			tags:    port.Tags,
			visible: port.ProjectID == projectID,
			owned:   port.ProjectID == projectID,
			store:   func(tags []string) { port.Tags = tags; s.ports[id] = port },
		}, ok
	case TagRouter:
		router, ok := s.routers[id]
		return taggedResource{
			tags:    router.Tags,
			visible: router.ProjectID == projectID,
			owned:   router.ProjectID == projectID,
			store:   func(tags []string) { router.Tags = tags; s.routers[id] = router },
		}, ok
	case TagSecurityGroup:
		group, ok := s.securityGroups[id]
		return taggedResource{
			tags:    group.Tags,
			visible: group.ProjectID == projectID,
			owned:   group.ProjectID == projectID,
			store:   func(tags []string) { group.Tags = tags; s.securityGroups[id] = group },
		}, ok
	case TagFloatingIP:
		floatingIP, ok := s.floatingIPs[id]
		return taggedResource{
			tags:    floatingIP.Tags,
			visible: floatingIP.ProjectID == projectID,
			owned:   floatingIP.ProjectID == projectID,
			store:   func(tags []string) { floatingIP.Tags = tags; s.floatingIPs[id] = floatingIP },
		}, ok
	}
	return taggedResource{}, false
}

func (s *Service) readableTags(projectID string, kind TagResource, id string) (taggedResource, error) {
	resource, ok := s.taggedResource(projectID, kind, id)
	if !ok {
		return taggedResource{}, ErrTagResourceNotFound
	}
	if !resource.visible {
		return taggedResource{}, ErrTagResourceForbidden
	}
	return resource, nil
}

func (s *Service) writableTags(projectID string, kind TagResource, id string) (taggedResource, error) {
	resource, ok := s.taggedResource(projectID, kind, id)
	if !ok {
		return taggedResource{}, ErrTagResourceNotFound
	}
	if !resource.owned {
		return taggedResource{}, ErrTagResourceForbidden
	}
	return resource, nil
}

func (s *Service) ResourceTags(projectID string, kind TagResource, id string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	resource, err := s.readableTags(projectID, kind, id)
	if err != nil {
		return nil, err
	}
	return slices.Clone(resource.tags), nil
}

func (s *Service) CheckResourceTag(projectID string, kind TagResource, id, tag string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	resource, err := s.readableTags(projectID, kind, id)
	if err != nil {
		return err
	}
	if !slices.Contains(resource.tags, tag) {
		return ErrTagNotFound
	}
	return nil
}

func (s *Service) ReplaceResourceTags(projectID string, kind TagResource, id string, tags []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	resource, err := s.writableTags(projectID, kind, id)
	if err != nil {
		return err
	}
	return storeTags(resource, tags)
}

func (s *Service) AddResourceTag(projectID string, kind TagResource, id, tag string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	resource, err := s.writableTags(projectID, kind, id)
	if err != nil {
		return err
	}
	return storeTags(resource, append(slices.Clone(resource.tags), tag))
}

func (s *Service) RemoveResourceTag(projectID string, kind TagResource, id, tag string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	resource, err := s.writableTags(projectID, kind, id)
	if err != nil {
		return err
	}
	if !slices.Contains(resource.tags, tag) {
		return ErrTagNotFound
	}
	remaining := slices.DeleteFunc(slices.Clone(resource.tags), func(existing string) bool {
		return existing == tag
	})
	return storeTags(resource, remaining)
}

func (s *Service) ClearResourceTags(projectID string, kind TagResource, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	resource, err := s.writableTags(projectID, kind, id)
	if err != nil {
		return err
	}
	return storeTags(resource, nil)
}

func storeTags(resource taggedResource, tags []string) error {
	unique := slices.Clone(tags)
	slices.Sort(unique)
	unique = slices.Compact(unique)
	if len(unique) > MaxResourceTags {
		return ErrTagLimitExceeded
	}
	if len(unique) == 0 {
		unique = nil
	}
	resource.store(unique)
	return nil
}
