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
	ErrImageNotFound       = errors.New("image not found")
	ErrImageDeactivated    = errors.New("image is deactivated")
	ErrDownloadUnavailable = errors.New("image download temporarily unavailable")
)

type Service struct {
	mu                  sync.RWMutex
	seed                domain.Image
	image               domain.Image
	now                 func() time.Time
	data                string
	downloadUnavailable bool
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

// CheckDownload returns the resource length without consuming a download fault.
func (s *Service) CheckDownload(projectID, imageID string) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if err := s.checkDownload(projectID, imageID); err != nil {
		return 0, err
	}
	return len(s.data), nil
}

func (s *Service) Download(projectID, imageID string) (data string, checksum string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.checkDownload(projectID, imageID); err != nil {
		return "", "", err
	}
	if s.downloadUnavailable {
		s.downloadUnavailable = false
		return "", "", ErrDownloadUnavailable
	}
	return s.data, s.image.Checksum, nil
}

// checkDownload validates the current image with the lock held.
func (s *Service) checkDownload(projectID, imageID string) error {
	if projectID != s.image.ProjectID || imageID != s.image.ID {
		return ErrImageNotFound
	}
	if s.image.Deactivated {
		return ErrImageDeactivated
	}
	return nil
}

// SetUnavailableDownload arms or cancels one failure for the next matching Download.
func (s *Service) SetUnavailableDownload(imageID string, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if imageID != s.image.ID {
		return ErrImageNotFound
	}
	s.downloadUnavailable = enabled
	return nil
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
	s.downloadUnavailable = false
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
