package compute

import (
	"regexp"

	domain "github.com/JSYoo5B/sandstack/internal/domain/compute"
)

type ListFilter struct {
	Status *domain.Status
	Name   *regexp.Regexp
}

type ServerSummary struct {
	ID   string
	Name string
}

func (s *Service) ListBasic(projectID string, filter ListFilter) []ServerSummary {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !filter.matches(projectID, s.server) {
		return []ServerSummary{}
	}
	return []ServerSummary{{ID: s.server.ID, Name: s.server.Name}}
}

func (s *Service) List(projectID string, filter ListFilter) []ServerDetail {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !filter.matches(projectID, s.server) {
		return []ServerDetail{}
	}
	return []ServerDetail{s.liveDetail()}
}

func (f ListFilter) matches(projectID string, server domain.Server) bool {
	return server.ProjectID == projectID &&
		(f.Status == nil || server.Status() == *f.Status) &&
		(f.Name == nil || f.Name.MatchString(server.Name))
}
