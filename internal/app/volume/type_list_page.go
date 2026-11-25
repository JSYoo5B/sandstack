package volume

import domain "github.com/JSYoo5B/sandstack/internal/domain/volume"

type VolumeTypePage struct {
	Types      []domain.VolumeType
	NextMarker string
}

// ListVolumeTypesPage returns the fixture type unless the page starts after it.
// Only the concrete type ID can be used as a marker.
func (s *Service) ListVolumeTypesPage(projectID string, marker *string, limit int) (VolumeTypePage, error) {
	if marker != nil && *marker != standardVolumeTypeID {
		return VolumeTypePage{}, ErrVolumeTypeNotFound
	}
	volumeType, err := s.GetVolumeType(projectID, standardVolumeTypeID)
	if err != nil && marker != nil {
		return VolumeTypePage{}, err
	}
	page := VolumeTypePage{Types: []domain.VolumeType{}}
	if err != nil || marker != nil || limit == 0 {
		return page, nil
	}
	page.Types = append(page.Types, volumeType)
	if limit == 1 {
		page.NextMarker = volumeType.ID
	}
	return page, nil
}
