package image

import (
	"crypto/md5"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	domain "github.com/JSYoo5B/sandstack/internal/domain/image"
)

var (
	ErrImageNotFound    = errors.New("image not found")
	ErrImageDeactivated = errors.New("image is deactivated")
)

type Service struct {
	mu    sync.RWMutex
	seed  domain.Image
	image domain.Image
	now   func() time.Time
	data  string
}

// New starts a service with one private project-owned image and a non-nil clock.
func New(seed domain.Image, now func() time.Time, data string) *Service {
	seed.Size = int64(len(data))
	seed.Checksum = ""
	if data != "" {
		// Glance v2.0 uses MD5 for its legacy checksum field.
		digest := md5.Sum([]byte(data))
		seed.Checksum = hex.EncodeToString(digest[:])
	}
	return &Service{seed: seed, image: seed, now: now, data: data}
}

func (s *Service) Get(projectID, imageID string) (domain.Image, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if projectID != s.image.ProjectID || imageID != s.image.ID {
		return domain.Image{}, ErrImageNotFound
	}
	return s.image, nil
}

type ListFilter struct {
	Name   *string
	Status *string
}

func (s *Service) List(projectID string, filter ListFilter) []domain.Image {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if projectID != s.image.ProjectID ||
		(filter.Name != nil && *filter.Name != s.image.Name) ||
		(filter.Status != nil && *filter.Status != s.image.Status()) {
		return []domain.Image{}
	}
	return []domain.Image{s.image}
}

func (s *Service) Download(projectID, imageID string) (data string, checksum string, err error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if projectID != s.image.ProjectID || imageID != s.image.ID {
		return "", "", ErrImageNotFound
	}
	if s.image.Deactivated {
		return "", "", ErrImageDeactivated
	}
	return s.data, s.image.Checksum, nil
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
