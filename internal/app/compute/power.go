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

func (s *Service) SetPower(request ActionRequest, serverID string, powered bool) error {
	if powered {
		return s.transition(request, serverID, domain.ActionStart, domain.StatusActive, domain.StatusShutoff)
	}
	return s.transition(request, serverID, domain.ActionStop,
		domain.StatusShutoff, domain.StatusActive, domain.StatusError)
}

func (s *Service) Reboot(request ActionRequest, serverID string, hard bool) error {
	if hard {
		return s.transition(request, serverID, domain.ActionReboot, domain.StatusActive, hardRebootStates...)
	}
	return s.transition(request, serverID, domain.ActionReboot, domain.StatusActive, domain.StatusActive)
}

func (s *Service) Pause(request ActionRequest, serverID string) error {
	return s.transition(request, serverID, domain.ActionPause, domain.StatusPaused, domain.StatusActive)
}

func (s *Service) Unpause(request ActionRequest, serverID string) error {
	return s.transition(request, serverID, domain.ActionUnpause, domain.StatusActive, domain.StatusPaused)
}

func (s *Service) Suspend(request ActionRequest, serverID string) error {
	return s.transition(request, serverID, domain.ActionSuspend, domain.StatusSuspended, domain.StatusActive)
}

func (s *Service) Resume(request ActionRequest, serverID string) error {
	return s.transition(request, serverID, domain.ActionResume, domain.StatusActive, domain.StatusSuspended)
}

func (s *Service) transition(
	request ActionRequest, serverID, action string, target domain.Status, allowed ...domain.Status,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	record, ok := s.find(request.ProjectID, serverID)
	if !ok {
		return ErrServerNotFound
	}
	server := &record.server
	if server.Locked {
		return ErrServerLocked
	}
	if !slices.Contains(allowed, server.Status()) {
		return ErrServerPowerState
	}
	server.State = target
	server.Fault = nil
	server.UpdatedAt = s.now().UTC()
	s.recordAction(record, request, action)
	return nil
}
