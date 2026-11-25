package volume

import (
	"slices"
	"strings"

	domain "github.com/JSYoo5B/sandstack/internal/domain/volume"
)

type ListFilter struct {
	Name   *string
	Status *domain.State
}

func (f ListFilter) matches(volume domain.Volume) bool {
	return (f.Name == nil || (volume.Name.Valid && volume.Name.Value == *f.Name)) &&
		(f.Status == nil || volume.State == *f.Status)
}

type VolumePage struct {
	Volumes    []domain.Volume
	NextMarker string
}

// List returns the project's matching volumes ordered by ID.
func (s *Service) List(projectID string, filter ListFilter) []domain.Volume {
	page, _ := s.ListPage(projectID, filter, nil, -1)
	return page.Volumes
}

// ListPage returns matching volumes after the marker in ID order. A negative
// limit is unbounded. A full positive page exposes its last ID as the next marker.
func (s *Service) ListPage(projectID string, filter ListFilter, marker *string, limit int) (VolumePage, error) {
	if s.seed.ProjectID != projectID {
		if marker != nil {
			return VolumePage{}, ErrVolumeNotFound
		}
		return VolumePage{Volumes: []domain.Volume{}}, nil
	}
	s.mu.RLock()
	var after string
	if marker != nil {
		after = *marker
		if _, ok := s.volumes[after]; !ok || after == "" {
			s.mu.RUnlock()
			return VolumePage{}, ErrVolumeNotFound
		}
	}
	if limit == 0 {
		s.mu.RUnlock()
		return VolumePage{Volumes: []domain.Volume{}}, nil
	}
	volumes := make([]domain.Volume, 0, len(s.volumes))
	for _, volume := range s.volumes {
		if (marker == nil || volume.ID > after) && filter.matches(volume) {
			volumes = append(volumes, volume)
		}
	}
	s.mu.RUnlock()

	slices.SortFunc(volumes, func(a, b domain.Volume) int { return strings.Compare(a.ID, b.ID) })
	if limit > 0 && len(volumes) >= limit {
		return VolumePage{Volumes: volumes[:limit], NextMarker: volumes[limit-1].ID}, nil
	}
	return VolumePage{Volumes: volumes}, nil
}
