package compute

import (
	"errors"
	"maps"
	"slices"
)

var ErrServerMetadatumNotFound = errors.New("server metadata key not found")

// GetMetadatum reads one live metadata item independently of detail faults.
func (s *Service) GetMetadatum(projectID, serverID, key string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	record, ok := s.find(projectID, serverID)
	if !ok {
		return "", ErrServerNotFound
	}
	value, found := record.server.Metadata[key]
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
	record, ok := s.find(projectID, serverID)
	if !ok {
		return ErrServerNotFound
	}
	server := &record.server
	if _, found := server.Metadata[key]; !found {
		return ErrServerMetadatumNotFound
	}
	if server.Locked {
		return ErrServerLocked
	}
	if !slices.Contains(metadataWriteStates, server.Status()) {
		return ErrServerMetadataState
	}
	metadata := maps.Clone(server.Metadata)
	delete(metadata, key)
	server.Metadata = metadata
	return nil
}
