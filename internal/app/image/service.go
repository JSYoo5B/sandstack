package image

import (
	"crypto/md5"
	"encoding/hex"
	"errors"
	"slices"
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
	seed.Tags = normalizeTags(seed.Tags)
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
	return snapshot(s.image), nil
}

type ImageUpdate struct {
	Name *string
	Tags []string
}

func (s *Service) Update(projectID, imageID string, changes ImageUpdate) (domain.Image, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if projectID != s.image.ProjectID || imageID != s.image.ID {
		return domain.Image{}, ErrImageNotFound
	}
	image := s.image
	if changes.Name != nil {
		image.Name = *changes.Name
	}
	if changes.Tags != nil {
		image.Tags = normalizeTags(changes.Tags)
	}
	if image.Name != s.image.Name || !slices.Equal(image.Tags, s.image.Tags) {
		image.UpdatedAt = s.now().UTC()
		s.image = image
	}
	return snapshot(s.image), nil
}

type ListFilter struct {
	Name   *string
	Status *string
	Tags   []string
}

func (s *Service) List(projectID string, filter ListFilter) []domain.Image {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if projectID != s.image.ProjectID ||
		(filter.Name != nil && *filter.Name != s.image.Name) ||
		(filter.Status != nil && *filter.Status != s.image.Status()) {
		return []domain.Image{}
	}
	for _, tag := range filter.Tags {
		if _, found := slices.BinarySearch(s.image.Tags, tag); !found {
			return []domain.Image{}
		}
	}
	return []domain.Image{snapshot(s.image)}
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

func normalizeTags(tags []string) []string {
	normalized := slices.Clone(tags)
	slices.Sort(normalized)
	return slices.Compact(normalized)
}

func snapshot(image domain.Image) domain.Image {
	image.Tags = slices.Clone(image.Tags)
	return image
}
