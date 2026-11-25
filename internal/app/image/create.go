package image

import (
	"errors"
	"regexp"

	domain "github.com/JSYoo5B/sandstack/internal/domain/image"
)

const imageIDAttempts = 8

var (
	ErrImageIDGeneration = errors.New("image ID generation failed")
	imageIDPattern       = regexp.MustCompile(`^[0-9a-f]{8}(-[0-9a-f]{4}){3}-[0-9a-f]{12}$`)
)

type CreateOptions struct {
	Name            string
	ContainerFormat string
	DiskFormat      string
	Protected       bool
}

// Create stores a queued image record before image bytes are available.
func (s *Service) Create(projectID string, options CreateOptions) (domain.Image, error) {
	if projectID != s.seed.ProjectID {
		return domain.Image{}, ErrImageNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	id, err := s.allocateID()
	if err != nil {
		return domain.Image{}, err
	}
	now := s.now().UTC()
	image := domain.Image{
		ID:              id,
		ProjectID:       projectID,
		Name:            options.Name,
		ContainerFormat: options.ContainerFormat,
		DiskFormat:      options.DiskFormat,
		CreatedAt:       now,
		UpdatedAt:       now,
		Queued:          true,
		Protected:       options.Protected,
	}
	s.images[id] = image
	return image, nil
}

// allocateID runs with the write lock held.
func (s *Service) allocateID() (string, error) {
	if s.newID == nil {
		return "", ErrImageIDGeneration
	}
	for range imageIDAttempts {
		id := s.newID()
		if !imageIDPattern.MatchString(id) {
			return "", ErrImageIDGeneration
		}
		_, exists := s.images[id]
		_, deleted := s.deletedIDs[id]
		if id != s.seed.ID && !exists && !deleted {
			return id, nil
		}
	}
	return "", ErrImageIDGeneration
}
