package compute

import (
	"errors"
	"slices"

	domain "github.com/JSYoo5B/sandstack/internal/domain/compute"
)

var ErrServerPowerState = errors.New("the server cannot perform this action in its current state")

var hardRebootStates = []domain.Status{
	domain.StatusActive, domain.StatusShutoff, domain.StatusPaused, domain.StatusSuspended, domain.StatusError,
}

func (s *Service) SetPower(projectID, serverID string, powered bool) error {
	if powered {
		return s.transition(projectID, serverID, domain.StatusActive, domain.StatusShutoff)
	}
	return s.transition(projectID, serverID, domain.StatusShutoff, domain.StatusActive, domain.StatusError)
}

func (s *Service) Reboot(projectID, serverID string, hard bool) error {
	if hard {
		return s.transition(projectID, serverID, domain.StatusActive, hardRebootStates...)
	}
	return s.transition(projectID, serverID, domain.StatusActive, domain.StatusActive)
}

func (s *Service) Pause(projectID, serverID string) error {
	return s.transition(projectID, serverID, domain.StatusPaused, domain.StatusActive)
}

func (s *Service) Unpause(projectID, serverID string) error {
	return s.transition(projectID, serverID, domain.StatusActive, domain.StatusPaused)
}

func (s *Service) Suspend(projectID, serverID string) error {
	return s.transition(projectID, serverID, domain.StatusSuspended, domain.StatusActive)
}

func (s *Service) Resume(projectID, serverID string) error {
	return s.transition(projectID, serverID, domain.StatusActive, domain.StatusSuspended)
}

func (s *Service) transition(projectID, serverID string, target domain.Status, allowed ...domain.Status) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.server.ProjectID != projectID || s.server.ID != serverID {
		return ErrServerNotFound
	}
	if !slices.Contains(allowed, s.server.Status()) {
		return ErrServerPowerState
	}
	s.server.State = target
	s.server.Fault = nil
	s.server.UpdatedAt = s.now().UTC()
	return nil
}
