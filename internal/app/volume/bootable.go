package volume

// SetBootable updates the flag and timestamp without consuming detail faults.
func (s *Service) SetBootable(projectID, volumeID string, bootable bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	volume, ok := s.volumes[volumeID]
	if !ok || volume.ProjectID != projectID {
		return ErrVolumeNotFound
	}
	volume.Bootable = bootable
	volume.UpdatedAt = s.now().UTC()
	s.volumes[volumeID] = volume
	return nil
}
