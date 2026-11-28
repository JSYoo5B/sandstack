package volume

import (
	"errors"
	"maps"

	domain "github.com/JSYoo5B/sandstack/internal/domain/volume"
)

var ErrVolumeSmallerThanSnapshot = errors.New("volume size must be at least the snapshot size")

func (s *Service) Create(projectID, userID, name, description string, size int) (domain.Volume, error) {
	return s.CreateWithMetadata(projectID, userID, name, description, size, nil)
}

func (s *Service) CreateWithMetadata(
	projectID, userID, name, description string,
	size int,
	metadata map[string]string,
) (domain.Volume, error) {
	return s.createBlank(owner{projectID, userID}, "", name, description, size, metadata)
}

func (s *Service) createBlank(
	creator owner, typeID, name, description string,
	size int,
	metadata map[string]string,
) (domain.Volume, error) {
	if size <= 0 {
		return domain.Volume{}, ErrInvalidVolumeSize
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.createVolume(creator, name, description, size, metadata, volumeSource{typeID: typeID})
}

// CreateFromSnapshot uses the snapshot's size when size is omitted (zero).
func (s *Service) CreateFromSnapshot(
	projectID, userID, snapshotID, name, description string,
	size int,
) (domain.Volume, error) {
	return s.CreateFromSnapshotWithMetadata(projectID, userID, snapshotID, name, description, size, nil)
}

// CreateFromSnapshotWithMetadata assigns the requested metadata to the new volume.
func (s *Service) CreateFromSnapshotWithMetadata(
	projectID, userID, snapshotID, name, description string,
	size int,
	metadata map[string]string,
) (domain.Volume, error) {
	return s.createFromSnapshot(owner{projectID, userID}, "", snapshotID, name, description, size, metadata)
}

func (s *Service) createFromSnapshot(
	creator owner, typeID, snapshotID, name, description string,
	size int,
	metadata map[string]string,
) (domain.Volume, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	snapshot, ok := s.ownedSnapshot(creator.projectID, snapshotID)
	if !ok {
		return domain.Volume{}, ErrSnapshotNotFound
	}
	if snapshot.State != domain.StateAvailable {
		return domain.Volume{}, ErrSnapshotStateConflict
	}
	if size == 0 {
		size = snapshot.Size
	}
	if size <= 0 {
		return domain.Volume{}, ErrInvalidVolumeSize
	}
	if size < snapshot.Size {
		return domain.Volume{}, ErrVolumeSmallerThanSnapshot
	}
	if typeID == "" {
		typeID = s.volumes[snapshot.VolumeID].TypeID
	}
	return s.createVolume(creator, name, description, size, metadata, volumeSource{snapshotID: snapshotID, typeID: typeID})
}

type owner struct {
	projectID string
	userID    string
}

type volumeSource struct {
	snapshotID string
	volumeID   string
	image      domain.ImageSource
	bootable   bool
	typeID     string
}

// createVolume runs with the service's write lock held.
func (s *Service) createVolume(
	creator owner,
	name, description string,
	size int,
	metadata map[string]string,
	source volumeSource,
) (domain.Volume, error) {
	id, err := s.allocateID()
	if err != nil {
		return domain.Volume{}, err
	}
	volume := domain.Volume{
		ID:                  id,
		Name:                domain.NullableText{Value: name, Valid: name != ""},
		Description:         domain.NullableText{Value: description, Valid: description != ""},
		ProjectID:           creator.projectID,
		UserID:              creator.userID,
		SnapshotID:          source.snapshotID,
		SourceVolumeID:      source.volumeID,
		Size:                size,
		TypeID:              source.typeID,
		CreatedAt:           s.now().UTC(),
		State:               domain.StateCreating,
		Metadata:            maps.Clone(metadata),
		ImageSource:         source.image,
		Bootable:            source.bootable,
		BootableOnProvision: source.bootable || source.image.ID != "",
	}
	s.volumes[id] = volume
	return cloneVolume(volume), nil
}

type CreateRequest struct {
	VolumeType     string
	ImageID        string
	SnapshotID     string
	SourceVolumeID string
	Name           string
	Description    string
	Size           int
	Metadata       map[string]string
}

// CreateVolume resolves the requested type before selecting the volume source.
// An omitted type inherits the source volume or snapshot type, or defaults to standard.
func (s *Service) CreateVolume(projectID, userID string, request CreateRequest) (domain.Volume, error) {
	typeID, err := resolveCreateVolumeType(request.VolumeType)
	if err != nil {
		return domain.Volume{}, err
	}
	creator := owner{projectID: projectID, userID: userID}
	switch {
	case request.ImageID != "":
		return s.createFromImage(creator, typeID, request.ImageID,
			request.Name, request.Description, request.Size, request.Metadata)
	case request.SnapshotID != "":
		return s.createFromSnapshot(creator, typeID, request.SnapshotID,
			request.Name, request.Description, request.Size, request.Metadata)
	case request.SourceVolumeID != "":
		return s.createFromVolume(creator, typeID, request.SourceVolumeID,
			request.Name, request.Description, request.Size, request.Metadata)
	default:
		return s.createBlank(creator, typeID, request.Name, request.Description, request.Size, request.Metadata)
	}
}
