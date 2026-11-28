package compute

import (
	"errors"

	domain "github.com/JSYoo5B/sandstack/internal/domain/compute"
)

var ErrServerLocked = errors.New("the server is locked")

func (s *Service) Lock(request ActionRequest, serverID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.server.ProjectID != request.ProjectID || s.server.ID != serverID {
		return ErrServerNotFound
	}
	if s.server.Locked {
		return nil
	}
	s.server.Locked = true
	s.server.UpdatedAt = s.now().UTC()
	s.recordAction(request, domain.ActionLock)
	return nil
}

func (s *Service) Unlock(request ActionRequest, serverID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.server.ProjectID != request.ProjectID || s.server.ID != serverID {
		return ErrServerNotFound
	}
	s.server.Locked = false
	s.server.UpdatedAt = s.now().UTC()
	s.recordAction(request, domain.ActionUnlock)
	return nil
}
