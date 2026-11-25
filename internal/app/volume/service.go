package volume

import (
	"errors"
	"sync"
	"time"

	domain "github.com/JSYoo5B/sandstack/internal/domain/volume"
)

var ErrVolumeNotFound = errors.New("volume not found")

type Service struct {
	seed domain.Volume
	now  func() time.Time

	mu     sync.RWMutex
	volume domain.Volume
}

// New takes a seeded volume and a non-nil clock.
func New(seed domain.Volume, now func() time.Time) *Service {
	return &Service{seed: seed, now: now, volume: seed}
}

func (s *Service) Get(projectID, volumeID string) (domain.Volume, error) {
	if s.seed.ProjectID != projectID || s.seed.ID != volumeID {
		return domain.Volume{}, ErrVolumeNotFound
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.volume, nil
}

func (s *Service) List(projectID string) []domain.Volume {
	if s.seed.ProjectID != projectID {
		return []domain.Volume{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return []domain.Volume{s.volume}
}

func (s *Service) SetFailed(volumeID string, failed bool) error {
	if s.seed.ID != volumeID {
		return ErrVolumeNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.volume.Failed != failed {
		updatedAt := s.now().UTC()
		s.volume.Failed = failed
		s.volume.UpdatedAt = updatedAt
	}
	return nil
}

func (s *Service) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.volume = s.seed
}
