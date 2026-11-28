package volume

import (
	"errors"
	"slices"
	"strings"

	domain "github.com/JSYoo5B/sandstack/internal/domain/volume"
)

var (
	ErrAttachmentNotFound       = errors.New("volume attachment not found")
	ErrInvalidAttachmentDevice  = errors.New("volume attachment device must be /dev/vd[b-z]")
	ErrAttachmentDeviceConflict = errors.New("requested volume attachment device is in use or none is free")
)

const (
	devicePrefix = "/dev/vd"
	rootDevice   = devicePrefix + "a"
)

// Attach associates an available volume with a server validated by the caller.
func (s *Service) Attach(projectID, serverID, volumeID, device string) (domain.Attachment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	volume, err := s.attachableVolume(projectID, serverID, volumeID)
	if err != nil {
		return domain.Attachment{}, err
	}
	device, err = s.attachmentDevice(serverID, device)
	if err != nil {
		return domain.Attachment{}, err
	}
	return s.attach(volume, serverID, device, domain.StateInUse)
}

func (s *Service) ReserveRoot(projectID, serverID, volumeID string) (domain.Attachment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	volume, err := s.attachableVolume(projectID, serverID, volumeID)
	if err != nil {
		return domain.Attachment{}, err
	}
	return s.attach(volume, serverID, rootDevice, domain.StateReserved)
}

func (s *Service) CompleteReservation(projectID, volumeID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	volume, ok := s.volumes[volumeID]
	if !ok || volume.ProjectID != projectID {
		return ErrVolumeNotFound
	}
	switch volume.State {
	case domain.StateReserved:
		s.updateState(volume, domain.StateInUse)
	case domain.StateInUse:
	default:
		return ErrVolumeStateConflict
	}
	return nil
}

func (s *Service) attachableVolume(projectID, serverID, volumeID string) (domain.Volume, error) {
	volume, ok := s.volumes[volumeID]
	if !ok || volume.ProjectID != projectID {
		return domain.Volume{}, ErrVolumeNotFound
	}
	if serverID == "" {
		return domain.Volume{}, ErrAttachmentNotFound
	}
	if volume.State != domain.StateAvailable {
		return domain.Volume{}, ErrVolumeStateConflict
	}
	return volume, nil
}

func (s *Service) attach(volume domain.Volume, serverID, device string, state domain.State) (domain.Attachment, error) {
	id, err := s.allocateID()
	if err != nil {
		return domain.Attachment{}, err
	}
	attachedAt := s.now().UTC()
	attachment := domain.Attachment{
		ID:         id,
		VolumeID:   volume.ID,
		ServerID:   serverID,
		Device:     device,
		AttachedAt: attachedAt,
	}
	volume.Attachment = attachment
	volume.State = state
	volume.UpdatedAt = attachedAt
	s.volumes[volume.ID] = volume
	s.attachmentVolumes[id] = volume.ID
	return attachment, nil
}

func (s *Service) GetAttachment(projectID, serverID, volumeID string) (domain.Attachment, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	volume, ok := s.volumes[volumeID]
	if !ok || volume.ProjectID != projectID {
		return domain.Attachment{}, ErrVolumeNotFound
	}
	if volume.Attachment.ID == "" || volume.Attachment.ServerID != serverID {
		return domain.Attachment{}, ErrAttachmentNotFound
	}
	return volume.Attachment, nil
}

// ListAttachments returns the server's attachments ordered by volume ID.
func (s *Service) ListAttachments(projectID, serverID string) []domain.Attachment {
	attachments := []domain.Attachment{}
	s.mu.RLock()
	for _, volumeID := range s.attachmentVolumes {
		volume := s.volumes[volumeID]
		if volume.ProjectID == projectID && volume.Attachment.ServerID == serverID {
			attachments = append(attachments, volume.Attachment)
		}
	}
	s.mu.RUnlock()
	slices.SortFunc(attachments, func(a, b domain.Attachment) int {
		return strings.Compare(a.VolumeID, b.VolumeID)
	})
	return attachments
}

func (s *Service) Detach(projectID, serverID, volumeID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	volume, ok := s.volumes[volumeID]
	if !ok || volume.ProjectID != projectID {
		return ErrVolumeNotFound
	}
	if volume.Attachment.ID == "" || volume.Attachment.ServerID != serverID {
		return ErrAttachmentNotFound
	}
	if volume.State == domain.StateBackingUp {
		return ErrVolumeStateConflict
	}
	attachmentID := volume.Attachment.ID
	volume.Attachment = domain.Attachment{}
	s.updateState(volume, domain.StateAvailable)
	delete(s.attachmentVolumes, attachmentID)
	return nil
}

// attachmentDevice runs with the service's write lock held.
func (s *Service) attachmentDevice(serverID, requested string) (string, error) {
	if requested != "" &&
		(!strings.HasPrefix(requested, devicePrefix) || len(requested) != len(devicePrefix)+1 ||
			requested[len(devicePrefix)] < 'b' || requested[len(devicePrefix)] > 'z') {
		return "", ErrInvalidAttachmentDevice
	}
	used := make(map[string]bool)
	for _, volumeID := range s.attachmentVolumes {
		attachment := s.volumes[volumeID].Attachment
		if attachment.ServerID == serverID {
			used[attachment.Device] = true
		}
	}
	if requested != "" {
		if used[requested] {
			return "", ErrAttachmentDeviceConflict
		}
		return requested, nil
	}
	for letter := byte('b'); letter <= 'z'; letter++ {
		candidate := devicePrefix + string(letter)
		if !used[candidate] {
			return candidate, nil
		}
	}
	return "", ErrAttachmentDeviceConflict
}
