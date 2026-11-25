package volume

import (
	"errors"
	"regexp"
	"sync"
	"time"

	domain "github.com/JSYoo5B/sandstack/internal/domain/volume"
)

var (
	ErrVolumeNotFound      = errors.New("volume not found")
	ErrVolumeStateConflict = errors.New("volume state does not permit the operation")
	ErrInvalidVolumeSize   = errors.New("volume size must be positive")
	ErrVolumeIDGeneration  = errors.New("volume ID generation failed")
	ErrDetailUnavailable   = errors.New("volume detail temporarily unavailable")
)

const volumeIDAttempts = 8

var volumeIDPattern = regexp.MustCompile(`^[0-9a-f]{8}(-[0-9a-f]{4}){3}-[0-9a-f]{12}$`)

type Service struct {
	mu                 sync.RWMutex
	seed               domain.Volume
	now                func() time.Time
	newID              func() string
	volumes            map[string]domain.Volume
	snapshots          map[string]domain.Snapshot
	attachmentVolumes  map[string]string
	extensions         map[string]int
	unavailableDetails sync.Map
}

// New takes a seeded volume and a non-nil clock. The generator supplies
// lowercase UUIDs for new resources, or an empty string on failure.
func New(seed domain.Volume, now func() time.Time, newID func() string) *Service {
	if seed.State == "" {
		seed.State = domain.StateAvailable
	}
	service := &Service{seed: seed, now: now, newID: newID}
	service.Reset()
	return service
}

func (s *Service) Get(projectID, volumeID string) (domain.Volume, error) {
	if s.seed.ProjectID != projectID {
		return domain.Volume{}, ErrVolumeNotFound
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	volume, ok := s.volumes[volumeID]
	if !ok {
		return domain.Volume{}, ErrVolumeNotFound
	}
	// Consume one failure while the read lock keeps the volume identity stable.
	if _, unavailable := s.unavailableDetails.LoadAndDelete(volumeID); unavailable {
		return domain.Volume{}, ErrDetailUnavailable
	}
	return volume, nil
}

// allocateID runs with the service's write lock held.
func (s *Service) allocateID() (string, error) {
	if s.newID == nil {
		return "", ErrVolumeIDGeneration
	}
	for range volumeIDAttempts {
		id := s.newID()
		if !volumeIDPattern.MatchString(id) {
			return "", ErrVolumeIDGeneration
		}
		if _, exists := s.volumes[id]; exists || id == s.seed.ID || id == s.seed.Attachment.ID {
			continue
		}
		if _, attached := s.attachmentVolumes[id]; attached {
			continue
		}
		if _, exists := s.snapshots[id]; exists {
			continue
		}
		return id, nil
	}
	return "", ErrVolumeIDGeneration
}

func (s *Service) Delete(projectID, volumeID string) error {
	if s.seed.ProjectID != projectID {
		return ErrVolumeNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	volume, ok := s.volumes[volumeID]
	if !ok {
		return ErrVolumeNotFound
	}
	for _, snapshot := range s.snapshots {
		if snapshot.VolumeID == volumeID {
			return ErrVolumeHasSnapshots
		}
	}
	if volume.State != domain.StateAvailable && volume.State != domain.StateError &&
		volume.State != domain.StateErrorExtending {
		return ErrVolumeStateConflict
	}
	delete(s.volumes, volumeID)
	s.unavailableDetails.Delete(volumeID)
	return nil
}

func (s *Service) CompleteCreation(volumeID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	volume, ok := s.volumes[volumeID]
	if !ok {
		return ErrVolumeNotFound
	}
	switch volume.State {
	case domain.StateCreating:
		s.updateState(volume, domain.StateAvailable)
	case domain.StateAvailable:
		return nil
	default:
		return ErrVolumeStateConflict
	}
	return nil
}

func (s *Service) SetFailed(volumeID string, failed bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	volume, ok := s.volumes[volumeID]
	if !ok {
		return ErrVolumeNotFound
	}
	switch {
	case failed && (volume.State == domain.StateInUse || volume.State == domain.StateExtending):
		return ErrVolumeStateConflict
	case failed && (volume.State == domain.StateCreating || volume.State == domain.StateAvailable):
		s.updateState(volume, domain.StateError)
	case !failed && (volume.State == domain.StateError || volume.State == domain.StateErrorExtending):
		s.updateState(volume, domain.StateAvailable)
	}
	return nil
}

func (s *Service) updateState(volume domain.Volume, state domain.State) {
	volume.UpdatedAt = s.now().UTC()
	volume.State = state
	s.volumes[volume.ID] = volume
}

func (s *Service) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.volumes = map[string]domain.Volume{s.seed.ID: s.seed}
	s.snapshots = make(map[string]domain.Snapshot)
	s.attachmentVolumes = make(map[string]string)
	s.extensions = make(map[string]int)
	s.unavailableDetails.Clear()
	if s.seed.Attachment.ID != "" {
		s.attachmentVolumes[s.seed.Attachment.ID] = s.seed.ID
	}
}
