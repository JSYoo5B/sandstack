package compute

// UpdateName changes only the server name and returns its current live detail.
func (s *Service) UpdateName(projectID, serverID string, name *string) (ServerDetail, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	record, ok := s.find(projectID, serverID)
	if !ok {
		return ServerDetail{}, ErrServerNotFound
	}
	if name != nil && record.server.Name != *name {
		record.server.Name = *name
		record.server.UpdatedAt = s.now().UTC()
	}
	return s.liveDetail(record), nil
}
