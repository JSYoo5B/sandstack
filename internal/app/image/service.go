package image

import (
	"errors"
	"maps"
	"slices"
	"sync"
	"time"

	domain "github.com/JSYoo5B/sandstack/internal/domain/image"
)

var (
	ErrImageNotFound        = errors.New("image not found")
	ErrImageDeactivated     = errors.New("image is deactivated")
	ErrImageProtected       = errors.New("image is protected")
	ErrDownloadUnavailable  = errors.New("image download temporarily unavailable")
	ErrImageMarkerForbidden = errors.New("image list marker is not accessible")
)

type Service struct {
	mu                  sync.RWMutex
	seed                domain.Image
	images              map[string]domain.Image
	deletedIDs          map[string]struct{}
	payloads            map[string]string
	members             map[string]map[string]domain.Member
	now                 func() time.Time
	seedData            string
	newID               func() string
	downloadUnavailable bool
}

// New starts a service with one seed image and a non-nil clock.
// The generator supplies lowercase UUIDs for created images.
func New(seed domain.Image, now func() time.Time, data string, newID func() string) *Service {
	seed.Tags = normalizeTags(seed.Tags)
	seed.Size = int64(len(data))
	seed.Checksum = ""
	if data != "" {
		seed.Checksum = payloadChecksum(data)
	}
	service := &Service{seed: seed, now: now, seedData: data, newID: newID}
	service.Reset()
	return service
}

func (s *Service) Get(projectID, imageID string) (domain.Image, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	image, err := s.visibleImage(projectID, imageID)
	if err != nil {
		return domain.Image{}, err
	}
	return snapshot(image), nil
}

type ImageUpdate struct {
	Name       *string
	Tags       []string
	Protected  *bool
	Visibility *string
}

func (s *Service) Update(projectID, imageID string, changes ImageUpdate) (domain.Image, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	current, err := s.ownedImage(projectID, imageID)
	if err != nil {
		return domain.Image{}, err
	}
	image := current
	if changes.Name != nil {
		image.Name = *changes.Name
	}
	if changes.Tags != nil {
		image.Tags = normalizeTags(changes.Tags)
	}
	if changes.Protected != nil {
		image.Protected = *changes.Protected
	}
	if changes.Visibility != nil {
		if err := checkVisibility(*changes.Visibility); err != nil {
			return domain.Image{}, err
		}
		image.Visibility = *changes.Visibility
	}
	if image.Name != current.Name || !slices.Equal(image.Tags, current.Tags) ||
		image.Protected != current.Protected || image.Visibility != current.Visibility {
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

	image, err := s.ownedImage(projectID, imageID)
	if err != nil {
		return err
	}
	if image.Protected {
		return ErrImageProtected
	}
	delete(s.images, imageID)
	delete(s.payloads, imageID)
	delete(s.members, imageID)
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
	if image.ID == s.seed.ID && s.downloadUnavailable {
		s.downloadUnavailable = false
		return "", "", ErrDownloadUnavailable
	}
	return s.payloads[imageID], image.Checksum, nil
}

// checkDownload validates the current image with the lock held.
func (s *Service) checkDownload(projectID, imageID string) (domain.Image, error) {
	image, err := s.visibleImage(projectID, imageID)
	if err != nil {
		return domain.Image{}, err
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
	s.payloads = map[string]string{s.seed.ID: s.seedData}
	s.deletedIDs = make(map[string]struct{})
	s.members = make(map[string]map[string]domain.Member)
	s.downloadUnavailable = false
}

func normalizeTags(tags []string) []string {
	normalized := slices.Clone(tags)
	slices.Sort(normalized)
	return slices.Compact(normalized)
}

func snapshot(image domain.Image) domain.Image {
	image.Tags = slices.Clone(image.Tags)
	image.Properties = maps.Clone(image.Properties)
	return image
}
