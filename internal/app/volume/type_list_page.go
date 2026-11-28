package volume

import (
	"slices"

	domain "github.com/JSYoo5B/sandstack/internal/domain/volume"
)

type VolumeTypePage struct {
	Types      []domain.VolumeType
	NextMarker string
}

// ListVolumeTypesPage returns the fixture types after the marker in fixture order.
// Only a concrete type ID can be used as a marker.
func (s *Service) ListVolumeTypesPage(marker *string, limit int) (VolumeTypePage, error) {
	page := VolumeTypePage{Types: []domain.VolumeType{}}
	start := 0
	if marker != nil {
		index := slices.IndexFunc(fixtureVolumeTypes, func(volumeType domain.VolumeType) bool {
			return volumeType.ID == *marker
		})
		if index < 0 {
			return VolumeTypePage{}, ErrVolumeTypeNotFound
		}
		start = index + 1
	}
	if limit == 0 {
		return page, nil
	}
	remaining := fixtureVolumeTypes[start:]
	if limit > 0 && len(remaining) >= limit {
		remaining = remaining[:limit]
		page.NextMarker = remaining[len(remaining)-1].ID
	}
	page.Types = append(page.Types, remaining...)
	return page, nil
}
