package compute

import (
	"errors"
	"maps"
	"slices"

	domain "github.com/JSYoo5B/sandstack/internal/domain/compute"
)

const maxMetadataItems = 128

var metadataWriteStates = []domain.Status{domain.StatusActive, domain.StatusPaused, domain.StatusShutoff}

var (
	ErrServerMetadataState = errors.New("the server cannot update metadata in its current state")
	ErrServerMetadataQuota = errors.New("the server metadata item quota is exceeded")
)

// GetMetadata reads live metadata independently of server detail faults.
func (s *Service) GetMetadata(projectID, serverID string) (map[string]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.server.ProjectID != projectID || s.server.ID != serverID {
		return nil, ErrServerNotFound
	}
	return copyMetadata(s.server.Metadata), nil
}

// ReplaceMetadata updates the complete metadata collection of a live server.
func (s *Service) ReplaceMetadata(projectID, serverID string, values map[string]string) (map[string]string, error) {
	return s.writeMetadata(projectID, serverID, values, true)
}

// MergeMetadata updates the supplied keys in the metadata of a live server.
func (s *Service) MergeMetadata(projectID, serverID string, values map[string]string) (map[string]string, error) {
	return s.writeMetadata(projectID, serverID, values, false)
}

func (s *Service) writeMetadata(
	projectID, serverID string,
	values map[string]string,
	replace bool,
) (map[string]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.server.ProjectID != projectID || s.server.ID != serverID {
		return nil, ErrServerNotFound
	}
	if !slices.Contains(metadataWriteStates, s.server.Status()) {
		return nil, ErrServerMetadataState
	}
	if !replace {
		merged := copyMetadata(s.server.Metadata)
		maps.Copy(merged, values)
		values = merged
	}
	if len(values) > maxMetadataItems {
		return nil, ErrServerMetadataQuota
	}
	if !maps.Equal(s.server.Metadata, values) {
		s.server.Metadata = copyMetadata(values)
		s.server.UpdatedAt = s.now().UTC()
	}
	return copyMetadata(s.server.Metadata), nil
}

func copyMetadata(values map[string]string) map[string]string {
	metadata := make(map[string]string, len(values))
	maps.Copy(metadata, values)
	return metadata
}
