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

// https://github.com/openstack/glance/blob/12.0.0/glance/api/authorization.py#L115-L178
func TestOwnerSharesImageWhileOnlyTheMemberChangesStatus(t *testing.T) {
	const owner = "owner"
	const consumer = "consumer"
	seed := domain.Image{ID: "seed", ProjectID: owner, Name: "seed"}
	now := time.Date(2026, time.November, 28, 1, 2, 3, 0, time.UTC)
	service := app.New(seed, func() time.Time { return now }, "", nil)

	added, err := service.AddMember(owner, seed.ID, consumer)
	require.NoError(t, err)
	_, duplicateErr := service.AddMember(owner, seed.ID, consumer)
	_, foreignErr := service.AddMember(consumer, seed.ID, "other")
	_, missingImageErr := service.AddMember(owner, "missing", consumer)

	assert.Equal(t, domain.Member{
		ImageID: seed.ID, MemberID: consumer, Status: domain.MemberPending, CreatedAt: now, UpdatedAt: now,
	}, added)
	assert.ErrorIs(t, duplicateErr, app.ErrMemberExists)
	assert.ErrorIs(t, foreignErr, app.ErrImageNotFound)
	assert.ErrorIs(t, missingImageErr, app.ErrImageNotFound)

	now = now.Add(time.Minute)
	_, ownerUpdateErr := service.UpdateMember(owner, seed.ID, consumer, domain.MemberAccepted)
	_, ownerInvalidErr := service.UpdateMember(owner, seed.ID, consumer, "invalid")
	_, missingMemberErr := service.UpdateMember(owner, seed.ID, "missing", domain.MemberAccepted)
	stored, err := service.GetMember(owner, seed.ID, consumer)
	require.NoError(t, err)

	assert.ErrorIs(t, ownerUpdateErr, app.ErrMemberForbidden)
	assert.ErrorIs(t, ownerInvalidErr, app.ErrMemberForbidden)
	assert.ErrorIs(t, missingMemberErr, app.ErrMemberNotFound)
	assert.Equal(t, added, stored)
}

// https://github.com/openstack/glance/blob/12.0.0/glance/domain/__init__.py#L305-L335
func TestSelfMembershipStatusTransitions(t *testing.T) {
	const owner = "owner"
	seed := domain.Image{ID: "seed", ProjectID: owner, Name: "seed"}
	start := time.Date(2026, time.November, 28, 1, 2, 3, 0, time.UTC)
	now := start
	service := app.New(seed, func() time.Time { return now }, "", nil)
	_, err := service.AddMember(owner, seed.ID, owner)
	require.NoError(t, err)

	now = start.Add(time.Minute)
	accepted, err := service.UpdateMember(owner, seed.ID, owner, domain.MemberAccepted)
	require.NoError(t, err)
	now = start.Add(2 * time.Minute)
	unchanged, err := service.UpdateMember(owner, seed.ID, owner, domain.MemberAccepted)
	require.NoError(t, err)
	_, invalidErr := service.UpdateMember(owner, seed.ID, owner, "invalid")
	rejected, err := service.UpdateMember(owner, seed.ID, owner, domain.MemberRejected)
	require.NoError(t, err)

	assert.Equal(t, domain.MemberAccepted, accepted.Status)
	assert.Equal(t, start, accepted.CreatedAt)
	assert.Equal(t, start.Add(time.Minute), accepted.UpdatedAt)
	assert.Equal(t, accepted, unchanged)
	assert.ErrorIs(t, invalidErr, app.ErrMemberStatus)
	assert.Equal(t, domain.MemberRejected, rejected.Status)
	assert.Equal(t, start.Add(2*time.Minute), rejected.UpdatedAt)
}

// https://github.com/openstack/glance/blob/12.0.0/glance/quota/__init__.py#L175-L191
func TestImageMembersFollowLimitRemovalDeletionAndReset(t *testing.T) {
	const owner = "owner"
	const queuedID = "00000011-0000-4000-8000-000000000000"
	seed := domain.Image{ID: "seed", ProjectID: owner, Name: "seed"}
	now := time.Date(2026, time.November, 28, 1, 2, 3, 0, time.UTC)
	service := app.New(seed, func() time.Time { return now }, "", func() string { return queuedID })
	queued, err := service.Create(owner, app.CreateOptions{Name: "queued"})
	require.NoError(t, err)
	for i := range app.MaxImageMembers {
		_, err := service.AddMember(owner, seed.ID, fmt.Sprintf("project-%03d", i))
		require.NoError(t, err)
	}
	_, err = service.AddMember(owner, queued.ID, "consumer")
	require.NoError(t, err)

	_, limitErr := service.AddMember(owner, seed.ID, "project-000")
	removeErr := service.RemoveMember(owner, seed.ID, "project-000")
	repeatRemoveErr := service.RemoveMember(owner, seed.ID, "project-000")
	readded, readdErr := service.AddMember(owner, seed.ID, "project-000")
	members, err := service.ListMembers(owner, seed.ID)
	require.NoError(t, err)

	assert.ErrorIs(t, limitErr, app.ErrMemberLimit)
	assert.NoError(t, removeErr)
	assert.ErrorIs(t, repeatRemoveErr, app.ErrMemberNotFound)
	assert.NoError(t, readdErr)
	assert.Equal(t, domain.MemberPending, readded.Status)
	require.Len(t, members, app.MaxImageMembers)
	assert.Equal(t, "project-000", members[0].MemberID)
	assert.Equal(t, "project-127", members[len(members)-1].MemberID)

	require.NoError(t, service.Delete(owner, queued.ID))
	_, deletedErr := service.ListMembers(owner, queued.ID)
	service.Reset()
	recreated, err := service.Create(owner, app.CreateOptions{Name: "recreated"})
	require.NoError(t, err)
	require.Equal(t, queuedID, recreated.ID)
	seedMembers, err := service.ListMembers(owner, seed.ID)
	require.NoError(t, err)
	recreatedMembers, err := service.ListMembers(owner, recreated.ID)
	require.NoError(t, err)

	assert.ErrorIs(t, deletedErr, app.ErrImageNotFound)
	assert.Empty(t, seedMembers)
	assert.Empty(t, recreatedMembers)
}
