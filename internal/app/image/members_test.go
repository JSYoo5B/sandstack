package image_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	app "github.com/JSYoo5B/sandstack/internal/app/image"
	domain "github.com/JSYoo5B/sandstack/internal/domain/image"
)

const (
	memberOwner    = "owner"
	memberConsumer = "consumer"
	memberStranger = "stranger"
)

func sharedSeed() domain.Image {
	return domain.Image{ID: "seed", ProjectID: memberOwner, Name: "seed", Visibility: domain.VisibilityShared}
}

// https://github.com/openstack/glance/blob/3db7567fd77bad31d5fb2d484c7b8f6ff64eb8ef/glance/api/v2/image_members.py#L75-L232
// https://github.com/openstack/glance/blob/3db7567fd77bad31d5fb2d484c7b8f6ff64eb8ef/glance/policies/image.py#L223-L292
func TestOwnerSharesImageWhileOnlyTheMemberChangesStatus(t *testing.T) {
	seed := sharedSeed()
	now := time.Date(2026, time.November, 28, 1, 2, 3, 0, time.UTC)
	service := app.New(seed, func() time.Time { return now }, "", nil)

	added, err := service.AddMember(memberOwner, seed.ID, memberConsumer)
	require.NoError(t, err)
	_, duplicateErr := service.AddMember(memberOwner, seed.ID, memberConsumer)
	_, consumerAddErr := service.AddMember(memberConsumer, seed.ID, "other")
	_, strangerAddErr := service.AddMember(memberStranger, seed.ID, "other")
	_, missingImageErr := service.AddMember(memberOwner, "missing", memberConsumer)

	assert.Equal(t, domain.Member{
		ImageID: seed.ID, MemberID: memberConsumer, Status: domain.MemberPending, CreatedAt: now, UpdatedAt: now,
	}, added)
	assert.ErrorIs(t, duplicateErr, app.ErrMemberExists)
	assert.ErrorIs(t, consumerAddErr, app.ErrMemberForbidden)
	assert.ErrorIs(t, strangerAddErr, app.ErrImageNotFound)
	assert.ErrorIs(t, missingImageErr, app.ErrImageNotFound)

	now = now.Add(time.Minute)
	_, ownerUpdateErr := service.UpdateMember(memberOwner, seed.ID, memberConsumer, domain.MemberAccepted)
	_, ownerInvalidErr := service.UpdateMember(memberOwner, seed.ID, memberConsumer, "invalid")
	_, missingMemberErr := service.UpdateMember(memberOwner, seed.ID, "missing", domain.MemberAccepted)
	_, strangerUpdateErr := service.UpdateMember(memberStranger, seed.ID, memberConsumer, domain.MemberAccepted)
	stored, err := service.GetMember(memberOwner, seed.ID, memberConsumer)
	require.NoError(t, err)

	assert.ErrorIs(t, ownerUpdateErr, app.ErrMemberForbidden)
	assert.ErrorIs(t, ownerInvalidErr, app.ErrMemberForbidden)
	assert.ErrorIs(t, missingMemberErr, app.ErrMemberNotFound)
	assert.ErrorIs(t, strangerUpdateErr, app.ErrImageNotFound)
	assert.Equal(t, added, stored)

	accepted, err := service.UpdateMember(memberConsumer, seed.ID, memberConsumer, domain.MemberAccepted)
	require.NoError(t, err)
	_, consumerInvalidErr := service.UpdateMember(memberConsumer, seed.ID, memberConsumer, "invalid")

	assert.Equal(t, domain.MemberAccepted, accepted.Status)
	assert.Equal(t, now, accepted.UpdatedAt)
	assert.Equal(t, added.CreatedAt, accepted.CreatedAt)
	assert.ErrorIs(t, consumerInvalidErr, app.ErrMemberStatus)
}

// https://github.com/openstack/glance/blob/3db7567fd77bad31d5fb2d484c7b8f6ff64eb8ef/glance/db/sqlalchemy/api.py#L1427-L1448
func TestMemberSeesOnlyItsOwnMembership(t *testing.T) {
	seed := sharedSeed()
	service := app.New(seed, time.Now, "", nil)
	_, err := service.AddMember(memberOwner, seed.ID, memberConsumer)
	require.NoError(t, err)
	_, err = service.AddMember(memberOwner, seed.ID, "other")
	require.NoError(t, err)

	ownerMembers, err := service.ListMembers(memberOwner, seed.ID)
	require.NoError(t, err)
	consumerMembers, err := service.ListMembers(memberConsumer, seed.ID)
	require.NoError(t, err)
	own, ownErr := service.GetMember(memberConsumer, seed.ID, memberConsumer)
	_, otherErr := service.GetMember(memberConsumer, seed.ID, "other")
	_, otherUpdateErr := service.UpdateMember(memberConsumer, seed.ID, "other", domain.MemberAccepted)
	otherRemoveErr := service.RemoveMember(memberConsumer, seed.ID, "other")
	ownRemoveErr := service.RemoveMember(memberConsumer, seed.ID, memberConsumer)
	_, strangerListErr := service.ListMembers(memberStranger, seed.ID)

	assert.Len(t, ownerMembers, 2)
	require.Len(t, consumerMembers, 1)
	assert.Equal(t, memberConsumer, consumerMembers[0].MemberID)
	assert.NoError(t, ownErr)
	assert.Equal(t, memberConsumer, own.MemberID)
	assert.ErrorIs(t, otherErr, app.ErrMemberNotFound)
	assert.ErrorIs(t, otherUpdateErr, app.ErrMemberNotFound)
	assert.ErrorIs(t, otherRemoveErr, app.ErrMemberNotFound)
	assert.ErrorIs(t, ownRemoveErr, app.ErrMemberForbidden)
	assert.ErrorIs(t, strangerListErr, app.ErrImageNotFound)
}

// https://github.com/openstack/glance/blob/3db7567fd77bad31d5fb2d484c7b8f6ff64eb8ef/glance/domain/__init__.py#L319-L339
func TestMemberStatusTransitions(t *testing.T) {
	seed := sharedSeed()
	start := time.Date(2026, time.November, 28, 1, 2, 3, 0, time.UTC)
	now := start
	service := app.New(seed, func() time.Time { return now }, "", nil)
	_, err := service.AddMember(memberOwner, seed.ID, memberConsumer)
	require.NoError(t, err)

	now = start.Add(time.Minute)
	accepted, err := service.UpdateMember(memberConsumer, seed.ID, memberConsumer, domain.MemberAccepted)
	require.NoError(t, err)
	now = start.Add(2 * time.Minute)
	unchanged, err := service.UpdateMember(memberConsumer, seed.ID, memberConsumer, domain.MemberAccepted)
	require.NoError(t, err)
	rejected, err := service.UpdateMember(memberConsumer, seed.ID, memberConsumer, domain.MemberRejected)
	require.NoError(t, err)
	now = start.Add(3 * time.Minute)
	pending, err := service.UpdateMember(memberConsumer, seed.ID, memberConsumer, domain.MemberPending)
	require.NoError(t, err)

	assert.Equal(t, domain.MemberAccepted, accepted.Status)
	assert.Equal(t, start, accepted.CreatedAt)
	assert.Equal(t, start.Add(time.Minute), accepted.UpdatedAt)
	assert.Equal(t, accepted, unchanged)
	assert.Equal(t, domain.MemberRejected, rejected.Status)
	assert.Equal(t, start.Add(2*time.Minute), rejected.UpdatedAt)
	assert.Equal(t, domain.MemberPending, pending.Status)
	assert.Equal(t, start.Add(3*time.Minute), pending.UpdatedAt)
}

// https://github.com/openstack/glance/blob/3db7567fd77bad31d5fb2d484c7b8f6ff64eb8ef/glance/db/__init__.py#L103-L112
// https://github.com/openstack/glance/blob/3db7567fd77bad31d5fb2d484c7b8f6ff64eb8ef/glance/policies/base.py#L80-L82
func TestOwnerAddsItselfButCannotChangeItsOwnStatus(t *testing.T) {
	seed := sharedSeed()
	service := app.New(seed, time.Now, "", nil)

	self, err := service.AddMember(memberOwner, seed.ID, memberOwner)
	require.NoError(t, err)
	_, updateErr := service.UpdateMember(memberOwner, seed.ID, memberOwner, domain.MemberAccepted)
	stored, err := service.GetMember(memberOwner, seed.ID, memberOwner)
	require.NoError(t, err)

	assert.Equal(t, domain.MemberPending, self.Status)
	assert.ErrorIs(t, updateErr, app.ErrMemberForbidden)
	assert.Equal(t, self, stored)
}

// https://github.com/openstack/glance/blob/3db7567fd77bad31d5fb2d484c7b8f6ff64eb8ef/glance/api/v2/image_members.py#L75-L99
func TestOnlySharedImagesHaveMembers(t *testing.T) {
	seed := domain.Image{ID: "seed", ProjectID: memberOwner, Name: "seed", Visibility: domain.VisibilityPrivate}
	service := app.New(seed, time.Now, "", nil)

	_, addErr := service.AddMember(memberOwner, seed.ID, memberConsumer)
	_, listErr := service.ListMembers(memberOwner, seed.ID)
	_, getErr := service.GetMember(memberOwner, seed.ID, memberConsumer)
	_, updateErr := service.UpdateMember(memberOwner, seed.ID, memberConsumer, domain.MemberAccepted)
	removeErr := service.RemoveMember(memberOwner, seed.ID, memberConsumer)
	_, strangerErr := service.AddMember(memberConsumer, seed.ID, memberConsumer)

	assert.ErrorIs(t, addErr, app.ErrMemberVisibility)
	assert.ErrorIs(t, listErr, app.ErrMemberVisibility)
	assert.ErrorIs(t, getErr, app.ErrMemberVisibility)
	assert.ErrorIs(t, updateErr, app.ErrMemberVisibility)
	assert.ErrorIs(t, removeErr, app.ErrMemberVisibility)
	assert.ErrorIs(t, strangerErr, app.ErrImageNotFound)
}

// https://github.com/openstack/glance/blob/3db7567fd77bad31d5fb2d484c7b8f6ff64eb8ef/glance/api/v2/image_members.py#L126-L184
func TestImageMembersFollowLimitRemovalDeletionAndReset(t *testing.T) {
	const queuedID = "00000011-0000-4000-8000-000000000000"
	seed := sharedSeed()
	now := time.Date(2026, time.November, 28, 1, 2, 3, 0, time.UTC)
	service := app.New(seed, func() time.Time { return now }, "", func() string { return queuedID })
	queued, err := service.Create(memberOwner, app.CreateOptions{Name: "queued"})
	require.NoError(t, err)
	for i := range app.MaxImageMembers {
		_, err := service.AddMember(memberOwner, seed.ID, fmt.Sprintf("project-%03d", i))
		require.NoError(t, err)
	}
	_, err = service.AddMember(memberOwner, queued.ID, memberConsumer)
	require.NoError(t, err)

	_, limitErr := service.AddMember(memberOwner, seed.ID, "project-000")
	removeErr := service.RemoveMember(memberOwner, seed.ID, "project-000")
	repeatRemoveErr := service.RemoveMember(memberOwner, seed.ID, "project-000")
	readded, readdErr := service.AddMember(memberOwner, seed.ID, "project-000")
	members, err := service.ListMembers(memberOwner, seed.ID)
	require.NoError(t, err)

	assert.ErrorIs(t, limitErr, app.ErrMemberLimit)
	assert.NoError(t, removeErr)
	assert.ErrorIs(t, repeatRemoveErr, app.ErrMemberNotFound)
	assert.NoError(t, readdErr)
	assert.Equal(t, domain.MemberPending, readded.Status)
	require.Len(t, members, app.MaxImageMembers)
	assert.Equal(t, "project-000", members[0].MemberID)
	assert.Equal(t, "project-127", members[len(members)-1].MemberID)

	require.NoError(t, service.Delete(memberOwner, queued.ID))
	_, deletedErr := service.ListMembers(memberOwner, queued.ID)
	_, consumerDeletedErr := service.Get(memberConsumer, queued.ID)
	service.Reset()
	recreated, err := service.Create(memberOwner, app.CreateOptions{Name: "recreated"})
	require.NoError(t, err)
	require.Equal(t, queuedID, recreated.ID)
	seedMembers, err := service.ListMembers(memberOwner, seed.ID)
	require.NoError(t, err)
	recreatedMembers, err := service.ListMembers(memberOwner, recreated.ID)
	require.NoError(t, err)

	assert.ErrorIs(t, deletedErr, app.ErrImageNotFound)
	assert.ErrorIs(t, consumerDeletedErr, app.ErrImageNotFound)
	assert.Empty(t, seedMembers)
	assert.Empty(t, recreatedMembers)
}
