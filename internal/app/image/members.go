package image

import (
	"cmp"
	"errors"
	"slices"

	domain "github.com/JSYoo5B/sandstack/internal/domain/image"
)

const MaxImageMembers = 128

var (
	ErrMemberNotFound  = errors.New("image member not found")
	ErrMemberExists    = errors.New("image member already exists")
	ErrMemberForbidden = errors.New("image member status can be changed only by the member")
	ErrMemberLimit     = errors.New("image member limit exceeded")
	ErrMemberStatus    = errors.New("image member status must be pending, accepted, or rejected")
)

// AddMember shares an owned image with a project in the pending state.
func (s *Service) AddMember(projectID, imageID, memberID string) (domain.Member, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := s.ownedImage(projectID, imageID); err != nil {
		return domain.Member{}, err
	}
	members := s.members[imageID]
	if len(members) >= MaxImageMembers {
		return domain.Member{}, ErrMemberLimit
	}
	if _, exists := members[memberID]; exists {
		return domain.Member{}, ErrMemberExists
	}
	now := s.now().UTC()
	member := domain.Member{
		ImageID:   imageID,
		MemberID:  memberID,
		Status:    domain.MemberPending,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if members == nil {
		members = make(map[string]domain.Member)
		s.members[imageID] = members
	}
	members[memberID] = member
	return member, nil
}

func (s *Service) ListMembers(projectID, imageID string) ([]domain.Member, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if _, err := s.ownedImage(projectID, imageID); err != nil {
		return nil, err
	}
	members := make([]domain.Member, 0, len(s.members[imageID]))
	for _, member := range s.members[imageID] {
		members = append(members, member)
	}
	slices.SortFunc(members, func(a, b domain.Member) int {
		if order := a.CreatedAt.Compare(b.CreatedAt); order != 0 {
			return order
		}
		return cmp.Compare(a.MemberID, b.MemberID)
	})
	return members, nil
}

func (s *Service) GetMember(projectID, imageID, memberID string) (domain.Member, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.member(projectID, imageID, memberID)
}

// UpdateMember lets only the member project change its own status.
func (s *Service) UpdateMember(projectID, imageID, memberID, status string) (domain.Member, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	member, err := s.member(projectID, imageID, memberID)
	if err != nil {
		return domain.Member{}, err
	}
	if projectID != member.MemberID {
		return domain.Member{}, ErrMemberForbidden
	}
	switch status {
	case domain.MemberPending, domain.MemberAccepted, domain.MemberRejected:
	default:
		return domain.Member{}, ErrMemberStatus
	}
	if member.Status != status {
		member.Status = status
		member.UpdatedAt = s.now().UTC()
		s.members[imageID][memberID] = member
	}
	return member, nil
}

func (s *Service) RemoveMember(projectID, imageID, memberID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := s.member(projectID, imageID, memberID); err != nil {
		return err
	}
	delete(s.members[imageID], memberID)
	return nil
}

// member runs with the lock held.
func (s *Service) member(projectID, imageID, memberID string) (domain.Member, error) {
	if _, err := s.ownedImage(projectID, imageID); err != nil {
		return domain.Member{}, err
	}
	member, ok := s.members[imageID][memberID]
	if !ok {
		return domain.Member{}, ErrMemberNotFound
	}
	return member, nil
}

// ownedImage runs with the lock held.
func (s *Service) ownedImage(projectID, imageID string) (domain.Image, error) {
	image, ok := s.images[imageID]
	if !ok || projectID != image.ProjectID {
		return domain.Image{}, ErrImageNotFound
	}
	return image, nil
}
