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
	images             imageLookup
	volumes            map[string]domain.Volume
	snapshots          map[string]domain.Snapshot
	backups            map[string]domain.Backup
	transfers          map[string]domain.Transfer
	newAuthKey         func() string
	attachmentVolumes  map[string]string
	extensions         map[string]int
	unavailableDetails sync.Map
}

// New takes a seeded volume and a non-nil clock. The generator supplies
// lowercase UUIDs for new resources, or an empty string on failure.
func New(seed domain.Volume, now func() time.Time, newID func() string) *Service {
	return NewWithImageLookup(seed, now, newID, nil)
}

// NewWithImageLookup also enables creation from a scoped Glance image.
func NewWithImageLookup(seed domain.Volume, now func() time.Time, newID func() string, images imageLookup) *Service {
	if seed.State == "" {
		seed.State = domain.StateAvailable
	}
	service := &Service{seed: cloneVolume(seed), now: now, newID: newID, images: images}
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
	return cloneVolume(volume), nil
}

func (s *Service) Lookup(projectID, volumeID string) (domain.Volume, error) {
	if s.seed.ProjectID != projectID {
		return domain.Volume{}, ErrVolumeNotFound
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	volume, ok := s.volumes[volumeID]
	if !ok {
		return domain.Volume{}, ErrVolumeNotFound
	}
	return cloneVolume(volume), nil
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
		if _, exists := s.backups[id]; exists {
			continue
		}
		if _, exists := s.transfers[id]; exists {
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
		volume.State != domain.StateErrorExtending && volume.State != domain.StateErrorRestoring {
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
		if err := s.finishProvisioning(&volume); err != nil {
			volume.BootableOnProvision = false
			s.updateState(volume, domain.StateError)
			return err
		}
		s.updateState(volume, domain.StateAvailable)
	case domain.StateAvailable:
		return nil
	default:
		return ErrVolumeStateConflict
	}
	return nil
}

// finishProvisioning applies bootability and publishes image metadata with the write lock held.
func (s *Service) finishProvisioning(volume *domain.Volume) error {
	if volume.SnapshotID != "" {
		snapshot, found := s.snapshots[volume.SnapshotID]
		if !found {
			return ErrVolumeStateConflict
		}
		if original := s.volumes[snapshot.VolumeID]; original.Bootable {
			volume.Bootable = true
			volume.ImageSource = snapshot.ImageSource
		}
	}
	volume.ImageMetadataReady = volume.ImageSource.ID != ""
	if volume.BootableOnProvision {
		volume.Bootable = true
		volume.BootableOnProvision = false
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
	case failed && (volume.State == domain.StateInUse || volume.State == domain.StateExtending ||
		volume.State == domain.StateBackingUp || volume.State == domain.StateRestoringBackup ||
		volume.State == domain.StateAwaitingTransfer):
		return ErrVolumeStateConflict
	case failed && (volume.State == domain.StateCreating || volume.State == domain.StateAvailable):
		volume.BootableOnProvision = false
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
	s.backups = make(map[string]domain.Backup)
	s.transfers = make(map[string]domain.Transfer)
	s.attachmentVolumes = make(map[string]string)
	s.extensions = make(map[string]int)
	s.unavailableDetails.Clear()
	if s.seed.Attachment.ID != "" {
		s.attachmentVolumes[s.seed.Attachment.ID] = s.seed.ID
	}
}
