package volume

// SetUnavailableDetail schedules or cancels one failure for the next matching Get.
func (s *Service) SetUnavailableDetail(volumeID string, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.volumes[volumeID]; !ok {
		return ErrVolumeNotFound
	}
	if enabled {
		s.unavailableDetails.Store(volumeID, struct{}{})
	} else {
		s.unavailableDetails.Delete(volumeID)
	}
	return nil
}
