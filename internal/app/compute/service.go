package compute

import (
	"cmp"
	"errors"
	"maps"
	"slices"
	"strings"
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
	mu          sync.RWMutex
	seed        domain.Server
	seedActions []domain.Action
	servers     map[string]*serverRecord
	holdBuilds  bool
	now         func() time.Time
	attachments attachmentLookup
}

type serverRecord struct {
	server            domain.Server
	staleDetail       *ServerDetail
	detailUnavailable atomic.Bool
	actions           []domain.Action
	console           []byte
}

type attachmentLookup interface {
	ListAttachments(projectID, serverID string) []volumedomain.Attachment
}

// ServerDetail is the server and the resources visible at the time of the read.
type ServerDetail struct {
	domain.Server
	AttachedVolumeIDs []string
	Networks          []ServerNetwork
	SecurityGroups    []string
}

// New takes a seeded server, a non-nil clock, an optional attachment lookup,
// and the server's action history from oldest to newest.
func New(
	server domain.Server, now func() time.Time, attachments attachmentLookup, history ...domain.Action,
) *Service {
	s := &Service{
		seed:        cloneServer(server),
		seedActions: slices.Clone(history),
		now:         now,
		attachments: attachments,
	}
	s.restoreSeed()
	return s
}

func (s *Service) restoreSeed() {
	s.servers = map[string]*serverRecord{
		s.seed.ID: {
			server:  cloneServer(s.seed),
			actions: slices.Clone(s.seedActions),
			console: []byte(FixtureConsoleOutput),
		},
	}
	s.holdBuilds = false
}

// find returns the project's server with the compute lock already held.
func (s *Service) find(projectID, serverID string) (*serverRecord, bool) {
	record, ok := s.servers[serverID]
	if !ok || record.server.ProjectID != projectID {
		return nil, false
	}
	return record, true
}

func (s *Service) Get(projectID, serverID string) (ServerDetail, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	record, ok := s.find(projectID, serverID)
	if !ok {
		return ServerDetail{}, ErrServerNotFound
	}
	// The shared lock excludes rearming while concurrent reads try to consume.
	if record.detailUnavailable.Load() && record.detailUnavailable.Swap(false) {
		return ServerDetail{}, ErrDetailUnavailable
	}
	if record.staleDetail != nil {
		detail := *record.staleDetail
		detail.Server = cloneServer(detail.Server)
		detail.AttachedVolumeIDs = slices.Clone(detail.AttachedVolumeIDs)
		return detail, nil
	}
	return s.liveDetail(record), nil
}

// Reset restores the startup fixture, history, and console log, removes
// created servers, and clears detail read faults and held builds.
func (s *Service) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.restoreSeed()
}

// SetUnavailableDetail arms or cancels one failure for the next matching Get.
func (s *Service) SetUnavailableDetail(serverID string, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	record, ok := s.servers[serverID]
	if !ok {
		return ErrServerNotFound
	}
	record.detailUnavailable.Store(enabled)
	return nil
}

// SetStaleDetail freezes detail reads at the current live state until cleared.
// Repeating an enable keeps the original snapshot. Lists still expose live state.
func (s *Service) SetStaleDetail(serverID string, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	record, ok := s.servers[serverID]
	if !ok {
		return ErrServerNotFound
	}
	if !enabled {
		record.staleDetail = nil
		return nil
	}
	if record.staleDetail == nil {
		snapshot := s.liveDetail(record)
		record.staleDetail = &snapshot
	}
	return nil
}

// SetFault changes the server's fault and update timestamp on transitions.
// Repeating the current setting preserves both timestamps.
func (s *Service) SetFault(serverID string, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	record, ok := s.servers[serverID]
	if !ok {
		return ErrServerNotFound
	}
	server := &record.server
	if enabled == (server.Fault != nil) {
		return nil
	}

	updatedAt := s.now().UTC()
	if enabled {
		server.Fault = &domain.Fault{
			Code:      500,
			Message:   "Injected server fault",
			CreatedAt: updatedAt,
		}
	} else {
		server.Fault = nil
	}
	server.UpdatedAt = updatedAt
	return nil
}

// liveDetail reads attachments with the compute lock already held.
func (s *Service) liveDetail(record *serverRecord) ServerDetail {
	detail := ServerDetail{Server: cloneServer(record.server)}
	if s.attachments != nil {
		for _, attachment := range s.attachments.ListAttachments(record.server.ProjectID, record.server.ID) {
			detail.AttachedVolumeIDs = append(detail.AttachedVolumeIDs, attachment.VolumeID)
		}
	}
	return detail
}

// sortedRecords orders servers by creation time and then ID, newest first,
// as Nova's default list sort does.
func (s *Service) sortedRecords() []*serverRecord {
	return slices.SortedFunc(maps.Values(s.servers), func(a, b *serverRecord) int {
		return cmp.Or(
			b.server.CreatedAt.Compare(a.server.CreatedAt),
			strings.Compare(b.server.ID, a.server.ID),
		)
	})
}

func cloneServer(server domain.Server) domain.Server {
	server.Metadata = maps.Clone(server.Metadata)
	server.Ports = slices.Clone(server.Ports)
	if server.BootVolume != nil {
		bootVolume := *server.BootVolume
		server.BootVolume = &bootVolume
	}
	if server.Fault != nil {
		fault := *server.Fault
		server.Fault = &fault
	}
	return server
}
