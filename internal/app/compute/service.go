package compute

import (
	"errors"
	"maps"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	domain "github.com/JSYoo5B/sandstack/internal/domain/compute"
	volumedomain "github.com/JSYoo5B/sandstack/internal/domain/volume"
)

var (
	ErrServerNotFound    = errors.New("compute server not found")
	ErrDetailUnavailable = errors.New("compute server detail temporarily unavailable")
)

type Service struct {
	mu                sync.RWMutex
	seed              domain.Server
	server            domain.Server
	staleDetail       *ServerDetail
	detailUnavailable atomic.Bool
	now               func() time.Time
	attachments       attachmentLookup
}

type attachmentLookup interface {
	ListAttachments(projectID, serverID string) []volumedomain.Attachment
}

// ServerDetail is the server and the volume IDs visible at the time of the read.
type ServerDetail struct {
	domain.Server
	AttachedVolumeIDs []string
}

// New takes a seeded server, a non-nil clock, and an optional attachment lookup.
func New(server domain.Server, now func() time.Time, attachments attachmentLookup) *Service {
	return &Service{
		seed:        cloneServer(server),
		server:      cloneServer(server),
		now:         now,
		attachments: attachments,
	}
}

func (s *Service) Get(projectID, serverID string) (ServerDetail, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.server.ProjectID != projectID || s.server.ID != serverID {
		return ServerDetail{}, ErrServerNotFound
	}
	// The shared lock excludes rearming while concurrent reads try to consume.
	if s.detailUnavailable.Load() && s.detailUnavailable.Swap(false) {
		return ServerDetail{}, ErrDetailUnavailable
	}
	if s.staleDetail != nil {
		detail := *s.staleDetail
		detail.Server = cloneServer(detail.Server)
		detail.AttachedVolumeIDs = slices.Clone(detail.AttachedVolumeIDs)
		return detail, nil
	}
	return s.liveDetail(), nil
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
		snapshot := s.liveDetail()
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

// liveDetail reads attachments with the compute lock already held.
func (s *Service) liveDetail() ServerDetail {
	detail := ServerDetail{Server: cloneServer(s.server)}
	if s.attachments != nil {
		for _, attachment := range s.attachments.ListAttachments(s.server.ProjectID, s.server.ID) {
			detail.AttachedVolumeIDs = append(detail.AttachedVolumeIDs, attachment.VolumeID)
		}
	}
	return detail
}

func cloneServer(server domain.Server) domain.Server {
	server.Metadata = maps.Clone(server.Metadata)
	if server.Fault != nil {
		fault := *server.Fault
		server.Fault = &fault
	}
	return server
}
