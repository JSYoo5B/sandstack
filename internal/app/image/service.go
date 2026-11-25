package image

import (
	"errors"
	"sync"
	"time"

	domain "github.com/JSYoo5B/sandstack/internal/domain/image"
)

var ErrImageNotFound = errors.New("image not found")

type Service struct {
	mu    sync.RWMutex
	seed  domain.Image
	image domain.Image
	now   func() time.Time
}

// New starts a service with one private project-owned image and a non-nil clock.
func New(seed domain.Image, now func() time.Time) *Service {
	return &Service{seed: seed, image: seed, now: now}
}

func (s *Service) Get(projectID, imageID string) (domain.Image, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if projectID != s.image.ProjectID || imageID != s.image.ID {
		return domain.Image{}, ErrImageNotFound
	}
	return s.image, nil
}

// SetDeactivated changes the timestamp only when the state changes.
func (s *Service) SetDeactivated(imageID string, deactivated bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if imageID != s.image.ID {
		return ErrImageNotFound
	}
	if s.image.Deactivated == deactivated {
		return nil
	}
	updatedAt := s.now().UTC()
	s.image.Deactivated = deactivated
	s.image.UpdatedAt = updatedAt
	return nil
}

func (s *Service) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.image = s.seed
}
