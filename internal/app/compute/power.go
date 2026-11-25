package compute

import (
	"errors"

	domain "github.com/JSYoo5B/sandstack/internal/domain/compute"
)

var ErrServerPowerState = errors.New("the server cannot perform this action in its current state")

func (s *Service) SetPower(projectID, serverID string, powered bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.server.ProjectID != projectID || s.server.ID != serverID {
		return ErrServerNotFound
	}
	status := s.server.Status()
	canStart := powered && status == domain.StatusShutoff
	canStop := !powered && (status == domain.StatusActive || status == domain.StatusError)
	if !canStart && !canStop {
		return ErrServerPowerState
	}
	s.server.Stopped = !powered
	s.server.Fault = nil
	s.server.UpdatedAt = s.now().UTC()
	return nil
}
