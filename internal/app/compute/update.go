package compute

// UpdateName changes only the server name and returns its current live detail.
func (s *Service) UpdateName(projectID, serverID string, name *string) (ServerDetail, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.server.ProjectID != projectID || s.server.ID != serverID {
		return ServerDetail{}, ErrServerNotFound
	}
	if name != nil && s.server.Name != *name {
		s.server.Name = *name
		s.server.UpdatedAt = s.now().UTC()
	}
	return s.liveDetail(), nil
}
