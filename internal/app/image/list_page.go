package image

import (
	"cmp"
	"slices"

	domain "github.com/JSYoo5B/sandstack/internal/domain/image"
)

type ListFilter struct {
	IDs    map[string]struct{}
	Name   *string
	Status *string
	Tags   []string
}

func (f ListFilter) matches(image domain.Image) bool {
	if f.IDs != nil {
		if _, match := f.IDs[image.ID]; !match {
			return false
		}
	}
	if (f.Name != nil && *f.Name != image.Name) || (f.Status != nil && *f.Status != image.Status()) {
		return false
	}
	for _, tag := range f.Tags {
		if _, found := slices.BinarySearch(image.Tags, tag); !found {
			return false
		}
	}
	return true
}

type ImagePage struct {
	Images     []domain.Image
	NextMarker string
}

func (s *Service) List(projectID string, filter ListFilter) []domain.Image {
	page, _ := s.ListPage(projectID, filter, nil, -1)
	return page.Images
}

// ListPage resolves marker visibility before applying filters or limiting results.
func (s *Service) ListPage(projectID string, filter ListFilter, marker *string, limit int) (ImagePage, error) {
	s.mu.RLock()
	page := ImagePage{Images: []domain.Image{}}
	var anchor domain.Image
	if marker != nil {
		var found bool
		anchor, found = s.images[*marker]
		if !found || *marker == "" {
			s.mu.RUnlock()
			return ImagePage{}, ErrImageNotFound
		}
		if projectID != anchor.ProjectID {
			s.mu.RUnlock()
			return ImagePage{}, ErrImageMarkerForbidden
		}
	}
	if limit == 0 {
		s.mu.RUnlock()
		return page, nil
	}
	for _, image := range s.images {
		if projectID == image.ProjectID && (marker == nil || compareImages(image, anchor) > 0) && filter.matches(image) {
			page.Images = append(page.Images, image)
		}
	}
	s.mu.RUnlock()

	slices.SortFunc(page.Images, compareImages)
	if limit > 0 && len(page.Images) >= limit {
		page.Images = page.Images[:limit:limit]
		page.NextMarker = page.Images[limit-1].ID
	}
	for i, image := range page.Images {
		page.Images[i] = snapshot(image)
	}
	return page, nil
}

func compareImages(a, b domain.Image) int {
	if dateOrder := b.CreatedAt.Compare(a.CreatedAt); dateOrder != 0 {
		return dateOrder
	}
	return cmp.Compare(b.ID, a.ID)
}
