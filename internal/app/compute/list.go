package compute

import (
	"regexp"

	domain "github.com/JSYoo5B/sandstack/internal/domain/compute"
)

type ListFilter struct {
	Status *domain.Status
	Name   *regexp.Regexp
}

func (s *Service) List(projectID string, filter ListFilter) []ServerDetail {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.server.ProjectID != projectID ||
		(filter.Status != nil && s.server.Status() != *filter.Status) ||
		(filter.Name != nil && !filter.Name.MatchString(s.server.Name)) {
		return []ServerDetail{}
	}
	return []ServerDetail{s.liveDetail()}
}
