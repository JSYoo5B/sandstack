package volume

import (
	"errors"

	domain "github.com/JSYoo5B/sandstack/internal/domain/volume"
)

const (
	// StandardVolumeTypeName is the fixture's default volume type name.
	StandardVolumeTypeName = "standard"
	standardVolumeTypeID   = "3190e22a-9606-4cf7-b9bd-4eb5af3dc601"
	// PerformanceVolumeTypeName is the fixture's alternative volume type name.
	PerformanceVolumeTypeName = "performance"
	performanceVolumeTypeID   = "6f0c2b8e-4d1a-4e7b-9c35-2a8f1d7e5b90"
)

var ErrVolumeTypeNotFound = errors.New("volume type not found")

var fixtureVolumeTypes = []domain.VolumeType{
	{
		ID:          standardVolumeTypeID,
		Name:        StandardVolumeTypeName,
		Description: "Standard SandStack volume type",
		Public:      true,
	},
	{
		ID:          performanceVolumeTypeID,
		Name:        PerformanceVolumeTypeName,
		Description: "Performance SandStack volume type",
		Public:      true,
	},
}

// resolveCreateVolumeType returns an empty ID when the volume inherits or defaults its type.
func (s *Service) resolveCreateVolumeType(projectID, selector string) (string, error) {
	if s.seed.ProjectID != projectID {
		return "", ErrVolumeTypeNotFound
	}
	if selector == "" {
		return "", nil
	}
	volumeType, ok := findVolumeType(selector)
	if !ok {
		return "", ErrVolumeTypeNotFound
	}
	return volumeType.ID, nil
}

// GetVolumeType resolves the fixture's default alias and concrete type IDs.
func (s *Service) GetVolumeType(projectID, typeID string) (domain.VolumeType, error) {
	if s.seed.ProjectID != projectID {
		return domain.VolumeType{}, ErrVolumeTypeNotFound
	}
	if typeID == "default" {
		typeID = standardVolumeTypeID
	}
	for _, volumeType := range fixtureVolumeTypes {
		if volumeType.ID == typeID {
			return volumeType, nil
		}
	}
	return domain.VolumeType{}, ErrVolumeTypeNotFound
}

// VolumeTypeName returns the fixture type name reported for a volume.
func VolumeTypeName(typeID string) string {
	volumeType, _ := findVolumeType(effectiveVolumeTypeID(typeID))
	return volumeType.Name
}

func effectiveVolumeTypeID(typeID string) string {
	if typeID == "" {
		return standardVolumeTypeID
	}
	return typeID
}

func findVolumeType(selector string) (domain.VolumeType, bool) {
	for _, volumeType := range fixtureVolumeTypes {
		if selector == volumeType.ID || selector == volumeType.Name {
			return volumeType, true
		}
	}
	return domain.VolumeType{}, false
}
