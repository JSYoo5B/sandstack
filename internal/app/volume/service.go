package volume

import (
	"errors"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	domain "github.com/JSYoo5B/sandstack/internal/domain/volume"
)

var (
	ErrVolumeNotFound      = errors.New("volume not found")
	ErrVolumeStateConflict = errors.New("volume state does not permit the operation")
	ErrInvalidVolumeSize   = errors.New("volume size must be positive")
	ErrVolumeIDGeneration  = errors.New("volume ID generation failed")
)

const volumeIDAttempts = 8

var volumeIDPattern = regexp.MustCompile(`^[0-9a-f]{8}(-[0-9a-f]{4}){3}-[0-9a-f]{12}$`)

type Service struct {
	mu      sync.RWMutex
	seed    domain.Volume
	now     func() time.Time
	newID   func() string
	volumes map[string]domain.Volume
}

// New takes a seeded volume and a non-nil clock. The generator supplies
// lowercase UUIDs for new volumes, or an empty string on failure.
func New(seed domain.Volume, now func() time.Time, newID func() string) *Service {
	if seed.State == "" {
		seed.State = domain.StateAvailable
	}
	return &Service{
		seed:    seed,
		now:     now,
		newID:   newID,
		volumes: map[string]domain.Volume{seed.ID: seed},
	}
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
	return volume, nil
}

// List returns the project's volumes ordered by ID.
func (s *Service) List(projectID string) []domain.Volume {
	if s.seed.ProjectID != projectID {
		return []domain.Volume{}
	}
	s.mu.RLock()
	volumes := make([]domain.Volume, 0, len(s.volumes))
	for _, volume := range s.volumes {
		volumes = append(volumes, volume)
	}
	s.mu.RUnlock()

	slices.SortFunc(volumes, func(a, b domain.Volume) int { return strings.Compare(a.ID, b.ID) })
	return volumes
}

func (s *Service) Create(projectID, name, description string, size int) (domain.Volume, error) {
	if s.seed.ProjectID != projectID {
		return domain.Volume{}, ErrVolumeNotFound
	}
	if size <= 0 {
		return domain.Volume{}, ErrInvalidVolumeSize
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	id, err := s.allocateID()
	if err != nil {
		return domain.Volume{}, err
	}
	volume := domain.Volume{
		ID:          id,
		Name:        name,
		Description: description,
		ProjectID:   projectID,
		UserID:      s.seed.UserID,
		Size:        size,
		CreatedAt:   s.now().UTC(),
		State:       domain.StateCreating,
	}
	s.volumes[id] = volume
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
		if _, exists := s.volumes[id]; exists || id == s.seed.ID {
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
	if volume.State != domain.StateAvailable && volume.State != domain.StateError {
		return ErrVolumeStateConflict
	}
	delete(s.volumes, volumeID)
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
	case failed && (volume.State == domain.StateCreating || volume.State == domain.StateAvailable):
		s.updateState(volume, domain.StateError)
	case !failed && volume.State == domain.StateError:
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
}
