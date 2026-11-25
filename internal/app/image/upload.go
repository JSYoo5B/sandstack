package image

import (
	"crypto/md5"
	"encoding/hex"
	"errors"

	domain "github.com/JSYoo5B/sandstack/internal/domain/image"
)

var (
	ErrImageUploadState   = errors.New("image must be queued for upload")
	ErrImageUploadFormats = errors.New("image container and disk formats are required for upload")
)

// CheckUpload checks upload eligibility before reading the payload.
func (s *Service) CheckUpload(projectID, imageID string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, err := s.checkUpload(projectID, imageID)
	return err
}

// Upload activates a queued image and publishes its bytes and checksum together.
func (s *Service) Upload(projectID, imageID, data string) error {
	checksum := payloadChecksum(data)
	s.mu.Lock()
	defer s.mu.Unlock()
	image, err := s.checkUpload(projectID, imageID)
	if err != nil {
		return err
	}
	image.Queued = false
	image.Size = int64(len(data))
	image.Checksum = checksum
	image.UpdatedAt = s.now().UTC()
	s.images[imageID] = image
	s.payloads[imageID] = data
	return nil
}

// checkUpload validates the current image with the lock held.
func (s *Service) checkUpload(projectID, imageID string) (domain.Image, error) {
	image, ok := s.images[imageID]
	if !ok || projectID != image.ProjectID {
		return domain.Image{}, ErrImageNotFound
	}
	if !image.Queued || image.Deactivated {
		return domain.Image{}, ErrImageUploadState
	}
	if image.ContainerFormat == "" || image.DiskFormat == "" {
		return domain.Image{}, ErrImageUploadFormats
	}
	return image, nil
}

func payloadChecksum(data string) string {
	// Glance v2.0 uses MD5 for its legacy checksum field.
	digest := md5.Sum([]byte(data))
	return hex.EncodeToString(digest[:])
}
