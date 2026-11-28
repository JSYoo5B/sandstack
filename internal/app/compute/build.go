package compute

import (
	"errors"

	domain "github.com/JSYoo5B/sandstack/internal/domain/compute"
)

var ErrBuildNotPending = errors.New("the server build cannot change to the requested outcome")

const buildFailureMessage = "No valid host was found. There are not enough hosts available."

// SetHoldBuilds keeps servers created afterwards in BUILD until FinishBuild.
func (s *Service) SetHoldBuilds(enabled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.holdBuilds = enabled
}

// FinishBuild completes a held build as ACTIVE or fails it as ERROR with a
// fault. Repeating the same outcome succeeds without changes.
func (s *Service) FinishBuild(serverID string, failed bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	record, ok := s.servers[serverID]
	if !ok {
		return ErrServerNotFound
	}
	server := &record.server
	if server.State != domain.StatusBuild {
		target := domain.StatusActive
		if failed {
			target = domain.StatusError
		}
		if server.Status() == target {
			return nil
		}
		return ErrBuildNotPending
	}
	now := s.now().UTC()
	server.State = domain.StatusActive
	if failed {
		server.State = domain.StatusError
		server.Fault = &domain.Fault{Code: 500, Message: buildFailureMessage, CreatedAt: now}
	}
	server.UpdatedAt = now
	return nil
}
