package compute

import (
	"errors"
	"slices"

	domain "github.com/JSYoo5B/sandstack/internal/domain/compute"
)

var ErrActionNotFound = errors.New("compute server action not found")

// ActionRequest identifies the caller and API request that starts a server action.
type ActionRequest struct {
	ProjectID string
	UserID    string
	RequestID string
}

// ListActions returns the server's action records from newest to oldest.
func (s *Service) ListActions(projectID, serverID string) ([]domain.Action, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.server.ProjectID != projectID || s.server.ID != serverID {
		return nil, ErrServerNotFound
	}
	actions := slices.Clone(s.actions)
	slices.Reverse(actions)
	return actions, nil
}

// GetAction returns the newest action record started by the request.
func (s *Service) GetAction(projectID, serverID, requestID string) (domain.Action, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.server.ProjectID != projectID || s.server.ID != serverID {
		return domain.Action{}, ErrServerNotFound
	}
	for _, action := range slices.Backward(s.actions) {
		if action.RequestID == requestID {
			return action, nil
		}
	}
	return domain.Action{}, ErrActionNotFound
}

// recordAction appends a record with the compute lock already held.
func (s *Service) recordAction(request ActionRequest, name string) {
	s.actions = append(s.actions, domain.Action{
		Name:      name,
		ServerID:  s.server.ID,
		RequestID: request.RequestID,
		ProjectID: request.ProjectID,
		UserID:    request.UserID,
		StartTime: s.now().UTC(),
	})
}
