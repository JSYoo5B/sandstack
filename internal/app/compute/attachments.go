package compute

import (
	"errors"
	"slices"

	computedomain "github.com/JSYoo5B/sandstack/internal/domain/compute"
	volumedomain "github.com/JSYoo5B/sandstack/internal/domain/volume"
)

var ErrServerAttachmentState = errors.New(
	"volume attachment changes require an active, paused, stopped, or resized server")

var attachStates = []computedomain.Status{
	computedomain.StatusActive, computedomain.StatusPaused, computedomain.StatusShutoff, computedomain.StatusResized,
}

type attachmentVolumes interface {
	attachmentLookup
	Attach(projectID, serverID, volumeID, device string) (volumedomain.Attachment, error)
	GetAttachment(projectID, serverID, volumeID string) (volumedomain.Attachment, error)
	Detach(projectID, serverID, volumeID string) error
}

// Attachments validates the server before changing or reading its volumes.
type Attachments struct {
	compute *Service
	volumes attachmentVolumes
}

func NewAttachments(compute *Service, volumes attachmentVolumes) *Attachments {
	return &Attachments{compute: compute, volumes: volumes}
}

func (a *Attachments) Attach(
	request ActionRequest, serverID, volumeID, device string,
) (volumedomain.Attachment, error) {
	// Hold the live server state steady until the volume mutation completes.
	a.compute.mu.Lock()
	defer a.compute.mu.Unlock()
	record, ok := a.compute.find(request.ProjectID, serverID)
	if !ok {
		return volumedomain.Attachment{}, ErrServerNotFound
	}
	if record.server.Locked {
		return volumedomain.Attachment{}, ErrServerLocked
	}
	if !slices.Contains(attachStates, record.server.Status()) {
		return volumedomain.Attachment{}, ErrServerAttachmentState
	}
	attachment, err := a.volumes.Attach(request.ProjectID, serverID, volumeID, device)
	if err != nil {
		return volumedomain.Attachment{}, err
	}
	a.compute.recordAction(record, request, computedomain.ActionAttachVolume)
	return attachment, nil
}

func (a *Attachments) Get(projectID, serverID, volumeID string) (volumedomain.Attachment, error) {
	if !a.ownsServer(projectID, serverID) {
		return volumedomain.Attachment{}, ErrServerNotFound
	}
	return a.volumes.GetAttachment(projectID, serverID, volumeID)
}

func (a *Attachments) List(projectID, serverID string) ([]volumedomain.Attachment, error) {
	if !a.ownsServer(projectID, serverID) {
		return nil, ErrServerNotFound
	}
	return a.volumes.ListAttachments(projectID, serverID), nil
}

func (a *Attachments) Detach(request ActionRequest, serverID, volumeID string) error {
	projectID := request.ProjectID
	a.compute.mu.Lock()
	defer a.compute.mu.Unlock()
	record, ok := a.compute.find(projectID, serverID)
	if !ok {
		return ErrServerNotFound
	}
	server := record.server
	if server.BootVolume != nil && server.BootVolume.VolumeID == volumeID {
		if _, err := a.volumes.GetAttachment(projectID, serverID, volumeID); err != nil {
			return err
		}
		return ErrRootVolumeDetach
	}
	if server.Locked || server.Status() == computedomain.StatusSuspended {
		if _, err := a.volumes.GetAttachment(projectID, serverID, volumeID); err != nil {
			return err
		}
		if server.Locked {
			return ErrServerLocked
		}
		return ErrServerAttachmentState
	}
	if err := a.volumes.Detach(projectID, serverID, volumeID); err != nil {
		return err
	}
	a.compute.recordAction(record, request, computedomain.ActionDetachVolume)
	return nil
}

func (a *Attachments) ownsServer(projectID, serverID string) bool {
	a.compute.mu.RLock()
	defer a.compute.mu.RUnlock()
	_, ok := a.compute.find(projectID, serverID)
	return ok
}
