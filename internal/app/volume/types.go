package volume

import (
	"errors"

	domain "github.com/JSYoo5B/sandstack/internal/domain/volume"
)

const (
	// StandardVolumeTypeName is the fixture's default volume type name.
	StandardVolumeTypeName = "standard"
	standardVolumeTypeID   = "3190e22a-9606-4cf7-b9bd-4eb5af3dc601"
)

var ErrVolumeTypeNotFound = errors.New("volume type not found")

// ValidateCreateVolumeType checks a volume creation selector in the fixture project.
func (s *Service) ValidateCreateVolumeType(projectID, selector string) error {
	if s.seed.ProjectID != projectID {
		return ErrVolumeTypeNotFound
	}
	switch selector {
	case "", StandardVolumeTypeName, standardVolumeTypeID:
		return nil
	default:
		return ErrVolumeTypeNotFound
	}
}

// GetVolumeType resolves the fixture's default alias and concrete type ID.
func (s *Service) GetVolumeType(projectID, typeID string) (domain.VolumeType, error) {
	if s.seed.ProjectID != projectID || (typeID != "default" && typeID != standardVolumeTypeID) {
		return domain.VolumeType{}, ErrVolumeTypeNotFound
	}
	return domain.VolumeType{
		ID:          standardVolumeTypeID,
		Name:        StandardVolumeTypeName,
		Description: "Standard SandStack volume type",
		Public:      true,
	}, nil
}
