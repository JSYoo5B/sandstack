package compute

import (
	"errors"
	"maps"

	domain "github.com/JSYoo5B/sandstack/internal/domain/compute"
)

var ErrServerMetadatumNotFound = errors.New("server metadata key not found")

// GetMetadatum reads one live metadata item independently of detail faults.
func (s *Service) GetMetadatum(projectID, serverID, key string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.server.ProjectID != projectID || s.server.ID != serverID {
		return "", ErrServerNotFound
	}
	value, found := s.server.Metadata[key]
	if !found {
		return "", ErrServerMetadatumNotFound
	}
	return value, nil
}

// PutMetadatum adds or replaces one item under the collection write rules.
func (s *Service) PutMetadatum(projectID, serverID, key, value string) error {
	_, err := s.MergeMetadata(projectID, serverID, map[string]string{key: value})
	return err
}

// DeleteMetadatum removes one item while preserving the server timestamp.
func (s *Service) DeleteMetadatum(projectID, serverID, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.server.ProjectID != projectID || s.server.ID != serverID {
		return ErrServerNotFound
	}
	if _, found := s.server.Metadata[key]; !found {
		return ErrServerMetadatumNotFound
	}
	status := s.server.Status()
	if status != domain.StatusActive && status != domain.StatusShutoff {
		return ErrServerMetadataState
	}
	metadata := maps.Clone(s.server.Metadata)
	delete(metadata, key)
	s.server.Metadata = metadata
	return nil
}
