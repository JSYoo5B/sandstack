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
	ErrImageNotFound        = errors.New("image not found")
	ErrImageDeactivated     = errors.New("image is deactivated")
	ErrDownloadUnavailable  = errors.New("image download temporarily unavailable")
	ErrImageMarkerForbidden = errors.New("image list marker is not accessible")
)

type Service struct {
	mu                  sync.RWMutex
	seed                domain.Image
	images              map[string]domain.Image
	deletedIDs          map[string]struct{}
	now                 func() time.Time
	data                string
	newID               func() string
	downloadUnavailable bool
}

// New starts a service with one private image and a non-nil clock.
// The generator supplies lowercase UUIDs for created images.
func New(seed domain.Image, now func() time.Time, data string, newID func() string) *Service {
	seed.Tags = normalizeTags(seed.Tags)
	seed.Size = int64(len(data))
	seed.Checksum = ""
	if data != "" {
		// Glance v2.0 uses MD5 for its legacy checksum field.
		digest := md5.Sum([]byte(data))
		seed.Checksum = hex.EncodeToString(digest[:])
	}
	service := &Service{seed: seed, now: now, data: data, newID: newID}
	service.Reset()
	return service
}

func (s *Service) Get(projectID, imageID string) (domain.Image, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	image, ok := s.images[imageID]
	if !ok || projectID != image.ProjectID {
		return domain.Image{}, ErrImageNotFound
	}
	return snapshot(image), nil
}

type ImageUpdate struct {
	Name *string
	Tags []string
}

func (s *Service) Update(projectID, imageID string, changes ImageUpdate) (domain.Image, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	current, ok := s.images[imageID]
	if !ok || projectID != current.ProjectID {
		return domain.Image{}, ErrImageNotFound
	}
	image := current
	if changes.Name != nil {
		image.Name = *changes.Name
	}
	if changes.Tags != nil {
		image.Tags = normalizeTags(changes.Tags)
	}
	if image.Name != current.Name || !slices.Equal(image.Tags, current.Tags) {
		image.UpdatedAt = s.now().UTC()
		s.images[imageID] = image
		return snapshot(image), nil
	}
	return snapshot(current), nil
}

// Delete removes an owned image and keeps its ID reserved until reset.
func (s *Service) Delete(projectID, imageID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	image, ok := s.images[imageID]
	if !ok || projectID != image.ProjectID {
		return ErrImageNotFound
	}
	delete(s.images, imageID)
	if imageID == s.seed.ID {
		s.downloadUnavailable = false
	} else {
		s.deletedIDs[imageID] = struct{}{}
	}
	return nil
}

// CheckDownload returns the resource length without consuming a download fault.
func (s *Service) CheckDownload(projectID, imageID string) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	image, err := s.checkDownload(projectID, imageID)
	if err != nil {
		return 0, err
	}
	return int(image.Size), nil
}

func (s *Service) Download(projectID, imageID string) (data string, checksum string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	image, err := s.checkDownload(projectID, imageID)
	if err != nil {
		return "", "", err
	}
	if image.ID != s.seed.ID {
		return "", "", nil
	}
	if s.downloadUnavailable {
		s.downloadUnavailable = false
		return "", "", ErrDownloadUnavailable
	}
	return s.data, image.Checksum, nil
}

// checkDownload validates the current image with the lock held.
func (s *Service) checkDownload(projectID, imageID string) (domain.Image, error) {
	image, ok := s.images[imageID]
	if !ok || projectID != image.ProjectID {
		return domain.Image{}, ErrImageNotFound
	}
	if image.Deactivated {
		return domain.Image{}, ErrImageDeactivated
	}
	return image, nil
}

// SetUnavailableDownload arms or cancels one failure for the next matching Download.
func (s *Service) SetUnavailableDownload(imageID string, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.images[imageID]; imageID != s.seed.ID || !ok {
		return ErrImageNotFound
	}
	s.downloadUnavailable = enabled
	return nil
}

// SetDeactivated changes the timestamp only when the state changes.
func (s *Service) SetDeactivated(imageID string, deactivated bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	image, ok := s.images[imageID]
	if imageID != s.seed.ID || !ok {
		return ErrImageNotFound
	}
	if image.Deactivated == deactivated {
		return nil
	}
	updatedAt := s.now().UTC()
	image.Deactivated = deactivated
	image.UpdatedAt = updatedAt
	s.images[imageID] = image
	return nil
}

func (s *Service) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.images = map[string]domain.Image{s.seed.ID: s.seed}
	s.deletedIDs = make(map[string]struct{})
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
