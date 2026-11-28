package compute

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	domain "github.com/JSYoo5B/sandstack/internal/domain/compute"
)

const gibibyte = 1 << 30

var ErrInvalidResize = errors.New("the resize request is invalid")

// ResizeError carries Nova's message for a resize rejected with 400.
type ResizeError struct {
	Message string
}

func (e *ResizeError) Error() string { return e.Message }

func (e *ResizeError) Unwrap() error { return ErrInvalidResize }

// LockedError carries Nova's message for an action on a locked server.
type LockedError struct {
	ServerID string
}

func (e *LockedError) Error() string { return fmt.Sprintf("Instance %s is locked", e.ServerID) }

func (e *LockedError) Unwrap() error { return ErrServerLocked }

func resizeError(format string, args ...any) error {
	return &ResizeError{Message: fmt.Sprintf(format, args...)}
}

// Resize moves an active or stopped server to another flavor and leaves it
// waiting for confirmation. A nil autoDiskConfig keeps the request silent
// about disk partitioning.
func (s *Servers) Resize(request ActionRequest, serverID, flavorID string, autoDiskConfig *bool) error {
	projectID := request.ProjectID
	s.compute.mu.Lock()
	defer s.compute.mu.Unlock()

	record, ok := s.compute.find(projectID, serverID)
	if !ok {
		return ErrServerNotFound
	}
	server := &record.server
	if server.Locked {
		return &LockedError{ServerID: server.ID}
	}
	if err := checkInstanceState("resize", *server, domain.StatusActive, domain.StatusShutoff); err != nil {
		return err
	}
	imageMeta := s.snapshotImageMeta(projectID, *server)
	if autoDiskConfig != nil && *autoDiskConfig &&
		strings.ToLower(strings.TrimSpace(imageMeta.Properties["auto_disk_config"])) == "disabled" {
		return resizeError("Requested image %s has automatic disk resize disabled.",
			imageMeta.Properties[baseImageRefProperty])
	}
	flavor, err := s.compute.GetFlavor(projectID, flavorID)
	if err != nil {
		return resizeError("Flavor %s could not be found.", flavorID)
	}
	if flavor.ID == server.FlavorID {
		return resizeError("When resizing, instances must change flavor!")
	}
	if flavor.RAMMiB < imageMeta.MinRAMMiB {
		return resizeError("Flavor's memory is too small for requested image.")
	}
	if server.BootVolume == nil && flavor.DiskGiB != 0 && imageMeta.MinDiskGiB > flavor.DiskGiB {
		return resizeError("Flavor's disk is smaller than the minimum size specified in image metadata. "+
			"Flavor disk is %d bytes, minimum size is %d bytes.",
			flavor.DiskGiB*gibibyte, imageMeta.MinDiskGiB*gibibyte)
	}

	server.Resize = &domain.ResizeOrigin{FlavorID: server.FlavorID, State: server.Status()}
	server.FlavorID = flavor.ID
	server.State = domain.StatusResized
	server.AutoDiskConfig = autoDiskConfig != nil && *autoDiskConfig
	server.UpdatedAt = s.compute.now().UTC()
	s.compute.recordAction(record, request, domain.ActionResize)
	return nil
}

// ConfirmResize keeps the new flavor and returns the server to the power
// state it had before the resize.
func (s *Service) ConfirmResize(request ActionRequest, serverID string) error {
	return s.finishResize(request, serverID, "confirmResize", domain.ActionConfirmResize, false)
}

// RevertResize restores the original flavor and state.
func (s *Service) RevertResize(request ActionRequest, serverID string) error {
	return s.finishResize(request, serverID, "revertResize", domain.ActionRevertResize, true)
}

func (s *Service) finishResize(request ActionRequest, serverID, method, action string, revert bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	record, ok := s.find(request.ProjectID, serverID)
	if !ok {
		return ErrServerNotFound
	}
	server := &record.server
	if server.Locked {
		return &LockedError{ServerID: server.ID}
	}
	if err := checkInstanceState(method, *server, domain.StatusResized); err != nil {
		return err
	}
	if revert {
		server.FlavorID = server.Resize.FlavorID
	}
	server.State = server.Resize.State
	server.Resize = nil
	server.UpdatedAt = s.now().UTC()
	s.recordAction(record, request, action)
	return nil
}

func checkInstanceState(method string, server domain.Server, allowed ...domain.Status) error {
	if status := server.Status(); !slices.Contains(allowed, status) {
		return &InvalidStateError{Message: fmt.Sprintf(
			"Cannot '%s' instance %s while it is in vm_state %s", method, server.ID, vmState(status))}
	}
	if server.TaskState != "" {
		return &InvalidStateError{Message: fmt.Sprintf(
			"Cannot '%s' instance %s while it is in task_state %s", method, server.ID, server.TaskState)}
	}
	return nil
}

func vmState(status domain.Status) string {
	switch status {
	case domain.StatusBuild:
		return "building"
	case domain.StatusShutoff:
		return "stopped"
	case domain.StatusResized:
		return "resized"
	default:
		return string(status)
	}
}
