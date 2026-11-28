package compute

import "errors"

var ErrServerLocked = errors.New("the server is locked")

func (s *Service) Lock(projectID, serverID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.server.ProjectID != projectID || s.server.ID != serverID {
		return ErrServerNotFound
	}
	if s.server.Locked {
		return nil
	}
	s.server.Locked = true
	s.server.UpdatedAt = s.now().UTC()
	return nil
}

func (s *Service) Unlock(projectID, serverID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.server.ProjectID != projectID || s.server.ID != serverID {
		return ErrServerNotFound
	}
	s.server.Locked = false
	s.server.UpdatedAt = s.now().UTC()
	return nil
}
