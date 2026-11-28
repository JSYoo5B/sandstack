package image

import (
	"errors"

	domain "github.com/JSYoo5B/sandstack/internal/domain/image"
)

const (
	VisibilityAll   = "all"
	MemberStatusAll = "all"
)

var (
	ErrImageForbidden  = errors.New("image is not owned by the project")
	ErrImagePublicize  = errors.New("only an administrator can make an image public")
	ErrImageVisibility = errors.New("image visibility must be community, public, private, or shared")
)

func checkVisibility(visibility string) error {
	switch visibility {
	case domain.VisibilityCommunity, domain.VisibilityShared, domain.VisibilityPrivate:
		return nil
	case domain.VisibilityPublic:
		return ErrImagePublicize
	default:
		return ErrImageVisibility
	}
}

// visible runs with the lock held.
func (s *Service) visible(projectID string, image domain.Image) bool {
	switch {
	case projectID == image.ProjectID:
		return true
	case image.Visibility == domain.VisibilityPublic, image.Visibility == domain.VisibilityCommunity:
		return true
	case image.Visibility == domain.VisibilityShared:
		_, member := s.members[image.ID][projectID]
		return member
	default:
		return false
	}
}

// visibleImage runs with the lock held.
func (s *Service) visibleImage(projectID, imageID string) (domain.Image, error) {
	image, ok := s.images[imageID]
	if !ok || !s.visible(projectID, image) {
		return domain.Image{}, ErrImageNotFound
	}
	return image, nil
}

// ownedImage runs with the lock held.
func (s *Service) ownedImage(projectID, imageID string) (domain.Image, error) {
	image, err := s.visibleImage(projectID, imageID)
	if err != nil {
		return domain.Image{}, err
	}
	if projectID != image.ProjectID {
		return domain.Image{}, ErrImageForbidden
	}
	return image, nil
}

// listed runs with the lock held.
func (s *Service) listed(projectID string, image domain.Image, filter ListFilter) bool {
	owned := projectID == image.ProjectID
	switch image.Visibility {
	case domain.VisibilityPublic, domain.VisibilityCommunity:
	case domain.VisibilityShared:
		if !owned && !s.memberStatusMatches(projectID, image.ID, filter.MemberStatus) {
			return false
		}
	default:
		if !owned {
			return false
		}
	}
	switch {
	case filter.Visibility != nil:
		if *filter.Visibility != VisibilityAll && *filter.Visibility != image.Visibility {
			return false
		}
	case image.Visibility == domain.VisibilityCommunity && !owned:
		return false
	}
	return filter.matches(image)
}

// memberStatusMatches runs with the lock held.
func (s *Service) memberStatusMatches(projectID, imageID, status string) bool {
	member, ok := s.members[imageID][projectID]
	if !ok {
		return false
	}
	if status == "" {
		status = domain.MemberAccepted
	}
	return status == MemberStatusAll || status == member.Status
}
