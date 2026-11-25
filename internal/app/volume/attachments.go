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

const devicePrefix = "/dev/vd"

// Attach associates an available volume with a server validated by the caller.
func (s *Service) Attach(projectID, serverID, volumeID, device string) (domain.Attachment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	volume, ok := s.volumes[volumeID]
	if !ok || volume.ProjectID != projectID {
		return domain.Attachment{}, ErrVolumeNotFound
	}
	if serverID == "" {
		return domain.Attachment{}, ErrAttachmentNotFound
	}
	if volume.State != domain.StateAvailable {
		return domain.Attachment{}, ErrVolumeStateConflict
	}
	device, err := s.attachmentDevice(serverID, device)
	if err != nil {
		return domain.Attachment{}, err
	}
	id, err := s.allocateID()
	if err != nil {
		return domain.Attachment{}, err
	}
	attachedAt := s.now().UTC()
	attachment := domain.Attachment{
		ID:         id,
		VolumeID:   volumeID,
		ServerID:   serverID,
		Device:     device,
		AttachedAt: attachedAt,
	}
	volume.Attachment = attachment
	volume.State = domain.StateInUse
	volume.UpdatedAt = attachedAt
	s.volumes[volumeID] = volume
	s.attachmentVolumes[id] = volumeID
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
	if s.seed.ProjectID != projectID {
		return attachments
	}
	s.mu.RLock()
	for _, volumeID := range s.attachmentVolumes {
		attachment := s.volumes[volumeID].Attachment
		if attachment.ServerID == serverID {
			attachments = append(attachments, attachment)
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
