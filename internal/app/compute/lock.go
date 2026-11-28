package compute

import (
	"errors"

	domain "github.com/JSYoo5B/sandstack/internal/domain/compute"
)

var ErrServerLocked = errors.New("the server is locked")

func (s *Service) Lock(request ActionRequest, serverID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	record, ok := s.find(request.ProjectID, serverID)
	if !ok {
		return ErrServerNotFound
	}
	if record.server.Locked {
		return nil
	}
	record.server.Locked = true
	record.server.UpdatedAt = s.now().UTC()
	s.recordAction(record, request, domain.ActionLock)
	return nil
}

func (s *Service) Unlock(request ActionRequest, serverID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	record, ok := s.find(request.ProjectID, serverID)
	if !ok {
		return ErrServerNotFound
	}
	record.server.Locked = false
	record.server.UpdatedAt = s.now().UTC()
	s.recordAction(record, request, domain.ActionUnlock)
	return nil
}
