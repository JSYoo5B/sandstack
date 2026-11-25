package compute

import (
	"errors"
	"sync"
	"sync/atomic"
	"time"

	domain "github.com/JSYoo5B/sandstack/internal/domain/compute"
)

var (
	ErrServerNotFound    = errors.New("compute server not found")
	ErrDetailUnavailable = errors.New("compute server detail temporarily unavailable")
)

type Service struct {
	mu                sync.RWMutex
	seed              domain.Server
	server            domain.Server
	staleDetail       *domain.Server
	detailUnavailable atomic.Bool
	now               func() time.Time
}

// New takes a seeded server and a non-nil clock.
func New(server domain.Server, now func() time.Time) *Service {
	return &Service{
		seed:   cloneServer(server),
		server: cloneServer(server),
		now:    now,
	}
}

func (s *Service) Get(projectID, serverID string) (domain.Server, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.server.ProjectID != projectID || s.server.ID != serverID {
		return domain.Server{}, ErrServerNotFound
	}
	// The shared lock excludes rearming while concurrent reads try to consume.
	if s.detailUnavailable.Load() && s.detailUnavailable.Swap(false) {
		return domain.Server{}, ErrDetailUnavailable
	}
	if s.staleDetail != nil {
		return cloneServer(*s.staleDetail), nil
	}
	return cloneServer(s.server), nil
}

func (s *Service) List(projectID string, status *domain.Status) []domain.Server {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.server.ProjectID != projectID || (status != nil && s.server.Status() != *status) {
		return []domain.Server{}
	}
	return []domain.Server{cloneServer(s.server)}
}

// Reset restores the startup fixture and clears detail read faults.
func (s *Service) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.server = cloneServer(s.seed)
	s.staleDetail = nil
	s.detailUnavailable.Store(false)
}

// SetUnavailableDetail arms or cancels one failure for the next matching Get.
func (s *Service) SetUnavailableDetail(serverID string, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.server.ID != serverID {
		return ErrServerNotFound
	}
	s.detailUnavailable.Store(enabled)
	return nil
}

// SetStaleDetail freezes detail reads at the current live state until cleared.
// Repeating an enable keeps the original snapshot. Lists still expose live state.
func (s *Service) SetStaleDetail(serverID string, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.server.ID != serverID {
		return ErrServerNotFound
	}
	if !enabled {
		s.staleDetail = nil
		return nil
	}
	if s.staleDetail == nil {
		snapshot := cloneServer(s.server)
		s.staleDetail = &snapshot
	}
	return nil
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
