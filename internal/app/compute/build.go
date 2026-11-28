package compute

import (
	"errors"
	"fmt"

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
func (s *Servers) FinishBuild(serverID string, failed bool) error {
	s.compute.mu.Lock()
	defer s.compute.mu.Unlock()

	record, ok := s.compute.servers[serverID]
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
	now := s.compute.now().UTC()
	server.State = domain.StatusActive
	server.UpdatedAt = now
	if groupID, ok := s.compute.memberGroupID(serverID); ok &&
		s.compute.groupHostTaken(server.ProjectID, groupID, serverID) {
		failed = true
	}
	if failed {
		server.State = domain.StatusError
		server.Fault = &domain.Fault{Code: 500, Message: buildFailureMessage, CreatedAt: now}
		return nil
	}
	if failure := s.prepareBootVolume(server.ProjectID, serverID, server.BootVolume); failure != "" {
		s.releasePorts(server.ProjectID, serverID, server.Ports)
		server.Ports = nil
		s.releaseBootVolume(server.ProjectID, serverID, server.BootVolume)
		server.State = domain.StatusError
		server.Fault = &domain.Fault{
			Code:      500,
			Message:   fmt.Sprintf("Build of instance %s aborted: %s", serverID, failure),
			CreatedAt: now,
		}
		return nil
	}
	server.Placed = true
	return nil
}
