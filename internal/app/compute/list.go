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

	summaries := []ServerSummary{}
	for _, record := range s.sortedRecords() {
		if filter.matches(projectID, record.server) {
			summaries = append(summaries, ServerSummary{ID: record.server.ID, Name: record.server.Name})
		}
	}
	return summaries
}

func (s *Service) List(projectID string, filter ListFilter) []ServerDetail {
	s.mu.RLock()
	defer s.mu.RUnlock()

	details := []ServerDetail{}
	for _, record := range s.sortedRecords() {
		if filter.matches(projectID, record.server) {
			details = append(details, s.liveDetail(record))
		}
	}
	return details
}

func (f ListFilter) matches(projectID string, server domain.Server) bool {
	return server.ProjectID == projectID &&
		(f.Status == nil || server.Status() == *f.Status) &&
		(f.Name == nil || f.Name.MatchString(server.Name))
}
