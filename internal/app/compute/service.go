package compute

import (
	"errors"
	"sync"
	"time"

	domain "github.com/JSYoo5B/sandstack/internal/domain/compute"
)

var ErrServerNotFound = errors.New("compute server not found")

type Service struct {
	mu     sync.RWMutex
	server domain.Server
	now    func() time.Time
}

// New takes a seeded server and a non-nil clock.
func New(server domain.Server, now func() time.Time) *Service {
	return &Service{server: cloneServer(server), now: now}
}

func (s *Service) Get(projectID, serverID string) (domain.Server, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.server.ProjectID != projectID || s.server.ID != serverID {
		return domain.Server{}, ErrServerNotFound
	}
	return cloneServer(s.server), nil
}

// SetFault changes the server's fault and update timestamp on transitions.
// Repeating the current setting preserves both timestamps.
func (s *Service) SetFault(serverID string, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.server.ID != serverID {
		return ErrServerNotFound
	}
	if enabled == (s.server.Fault != nil) {
		return nil
	}

	updatedAt := s.now().UTC()
	if enabled {
		s.server.Fault = &domain.Fault{
			Code:      500,
			Message:   "Injected server fault",
			CreatedAt: updatedAt,
		}
	} else {
		s.server.Fault = nil
	}
	s.server.UpdatedAt = updatedAt
	return nil
}

func cloneServer(server domain.Server) domain.Server {
	if server.Fault != nil {
		fault := *server.Fault
		server.Fault = &fault
	}
	return server
}
