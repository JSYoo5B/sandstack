package image_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	app "github.com/JSYoo5B/sandstack/internal/app/image"
	domain "github.com/JSYoo5B/sandstack/internal/domain/image"
)

type visibilityFixture struct {
	service   *app.Service
	public    domain.Image
	private   domain.Image
	community domain.Image
	accepted  domain.Image
	pending   domain.Image
	rejected  domain.Image
	foreign   domain.Image
}

func newVisibilityFixture(t *testing.T) visibilityFixture {
	t.Helper()
	ids := []string{
		"00000001-0000-4000-8000-000000000000", "00000002-0000-4000-8000-000000000000",
		"00000003-0000-4000-8000-000000000000", "00000004-0000-4000-8000-000000000000",
		"00000005-0000-4000-8000-000000000000", "00000006-0000-4000-8000-000000000000",
	}
	now := time.Date(2026, time.November, 28, 1, 2, 3, 0, time.UTC)
	public := domain.Image{
		ID: "00000000-0000-4000-8000-000000000000", ProjectID: memberOwner, Name: "public",
		Visibility: domain.VisibilityPublic, CreatedAt: now, UpdatedAt: now,
	}
	service := app.New(public, func() time.Time {
		now = now.Add(time.Minute)
		return now
	}, "public data", func() string {
		id := ids[0]
		ids = ids[1:]
		return id
	})
	create := func(projectID, name, visibility string) domain.Image {
		image, err := service.Create(projectID, app.CreateOptions{Name: name, Visibility: visibility})
		require.NoError(t, err)
		return image
	}
	fixture := visibilityFixture{service: service}
	fixture.public, _ = service.Get(memberOwner, public.ID)
	fixture.private = create(memberOwner, "private", domain.VisibilityPrivate)
	fixture.community = create(memberOwner, "community", domain.VisibilityCommunity)
	fixture.accepted = create(memberOwner, "accepted", "")
	fixture.pending = create(memberOwner, "pending", domain.VisibilityShared)
	fixture.rejected = create(memberOwner, "rejected", domain.VisibilityShared)
	fixture.foreign = create(memberConsumer, "foreign", domain.VisibilityPrivate)
	for _, image := range []domain.Image{fixture.accepted, fixture.pending, fixture.rejected} {
		_, err := service.AddMember(memberOwner, image.ID, memberConsumer)
		require.NoError(t, err)
	}
	_, err := service.UpdateMember(memberConsumer, fixture.accepted.ID, memberConsumer, domain.MemberAccepted)
	require.NoError(t, err)
	_, err = service.UpdateMember(memberConsumer, fixture.rejected.ID, memberConsumer, domain.MemberRejected)
	require.NoError(t, err)
	return fixture
}

func listedNames(t *testing.T, service *app.Service, projectID string, filter app.ListFilter) []string {
	t.Helper()
	page, err := service.ListPage(projectID, filter, nil, -1)
	require.NoError(t, err)
	names := make([]string, len(page.Images))
	for i, image := range page.Images {
		names[i] = image.Name
	}
	return names
}

func pointer[T any](value T) *T {
	return &value
}

// https://github.com/openstack/glance/blob/3db7567fd77bad31d5fb2d484c7b8f6ff64eb8ef/glance/domain/__init__.py#L71-L94
// https://github.com/openstack/glance/blob/3db7567fd77bad31d5fb2d484c7b8f6ff64eb8ef/glance/policies/base.py#L72-L74
// https://github.com/openstack/glance/blob/3db7567fd77bad31d5fb2d484c7b8f6ff64eb8ef/glance/policies/image.py#L94-L116
func TestAnyProjectCreatesSharedImagesByDefaultButNotPublicOnes(t *testing.T) {
	const createdID = "00000001-0000-4000-8000-000000000000"
	seed := domain.Image{ID: "seed", ProjectID: memberOwner, Visibility: domain.VisibilityPrivate}
	service := app.New(seed, time.Now, "", func() string { return createdID })

	created, err := service.Create(memberConsumer, app.CreateOptions{Name: "consumer image"})
	require.NoError(t, err)
	_, publicErr := service.Create(memberConsumer, app.CreateOptions{Name: "public", Visibility: domain.VisibilityPublic})
	_, invalidErr := service.Create(memberConsumer, app.CreateOptions{Name: "invalid", Visibility: "hidden"})
	_, ownerReadErr := service.Get(memberOwner, created.ID)

	assert.Equal(t, memberConsumer, created.ProjectID)
	assert.Equal(t, domain.VisibilityShared, created.Visibility)
	assert.ErrorIs(t, publicErr, app.ErrImagePublicize)
	assert.ErrorIs(t, invalidErr, app.ErrImageVisibility)
	assert.ErrorIs(t, ownerReadErr, app.ErrImageNotFound)
}

// https://github.com/openstack/glance/blob/3db7567fd77bad31d5fb2d484c7b8f6ff64eb8ef/glance/db/utils.py#L44-L74
func TestImageReadsFollowVisibilityAndMembership(t *testing.T) {
	fixture := newVisibilityFixture(t)
	tests := []struct {
		image    domain.Image
		consumer bool
		stranger bool
	}{
		{image: fixture.public, consumer: true, stranger: true},
		{image: fixture.community, consumer: true, stranger: true},
		{image: fixture.accepted, consumer: true},
		{image: fixture.pending, consumer: true},
		{image: fixture.rejected, consumer: true},
		{image: fixture.private},
	}
	for _, test := range tests {
		t.Run(test.image.Name, func(t *testing.T) {
			_, ownerErr := fixture.service.Get(memberOwner, test.image.ID)
			_, consumerErr := fixture.service.Get(memberConsumer, test.image.ID)
			_, strangerErr := fixture.service.Get(memberStranger, test.image.ID)

			assert.NoError(t, ownerErr)
			if test.consumer {
				assert.NoError(t, consumerErr)
			} else {
				assert.ErrorIs(t, consumerErr, app.ErrImageNotFound)
			}
			if test.stranger {
				assert.NoError(t, strangerErr)
			} else {
				assert.ErrorIs(t, strangerErr, app.ErrImageNotFound)
			}
		})
	}
}

// https://github.com/openstack/glance/blob/3db7567fd77bad31d5fb2d484c7b8f6ff64eb8ef/glance/db/sqlalchemy/api.py#L588-L625
// https://github.com/openstack/glance/blob/3db7567fd77bad31d5fb2d484c7b8f6ff64eb8ef/glance/db/sqlalchemy/api.py#L697-L713
func TestImageListsFollowVisibilityAndMemberStatus(t *testing.T) {
	fixture := newVisibilityFixture(t)
	service := fixture.service

	ownerDefault := listedNames(t, service, memberOwner, app.ListFilter{})
	consumerDefault := listedNames(t, service, memberConsumer, app.ListFilter{})
	strangerDefault := listedNames(t, service, memberStranger, app.ListFilter{})
	consumerPending := listedNames(t, service, memberConsumer, app.ListFilter{MemberStatus: domain.MemberPending})
	consumerAllMembers := listedNames(t, service, memberConsumer, app.ListFilter{MemberStatus: app.MemberStatusAll})
	consumerShared := listedNames(t, service, memberConsumer, app.ListFilter{
		Visibility: pointer(domain.VisibilityShared), MemberStatus: app.MemberStatusAll,
	})
	strangerCommunity := listedNames(t, service, memberStranger, app.ListFilter{
		Visibility: pointer(domain.VisibilityCommunity),
	})
	consumerPrivate := listedNames(t, service, memberConsumer, app.ListFilter{
		Visibility: pointer(domain.VisibilityPrivate),
	})
	consumerAll := listedNames(t, service, memberConsumer, app.ListFilter{Visibility: pointer(app.VisibilityAll)})

	assert.Equal(t, []string{"rejected", "pending", "accepted", "community", "private", "public"}, ownerDefault)
	assert.Equal(t, []string{"foreign", "accepted", "public"}, consumerDefault)
	assert.Equal(t, []string{"public"}, strangerDefault)
	assert.Equal(t, []string{"foreign", "pending", "public"}, consumerPending)
	assert.Equal(t, []string{"foreign", "rejected", "pending", "accepted", "public"}, consumerAllMembers)
	assert.Equal(t, []string{"rejected", "pending", "accepted"}, consumerShared)
	assert.Equal(t, []string{"community"}, strangerCommunity)
	assert.Equal(t, []string{"foreign"}, consumerPrivate)
	assert.Equal(t, []string{"foreign", "accepted", "community", "public"}, consumerAll)
}

// https://github.com/openstack/glance/blob/3db7567fd77bad31d5fb2d484c7b8f6ff64eb8ef/glance/db/sqlalchemy/api.py#L283-L310
func TestImageListMarkerMustBeVisible(t *testing.T) {
	fixture := newVisibilityFixture(t)

	pendingPage, pendingErr := fixture.service.ListPage(memberConsumer, app.ListFilter{}, &fixture.pending.ID, -1)
	_, privateErr := fixture.service.ListPage(memberConsumer, app.ListFilter{}, &fixture.private.ID, -1)

	assert.NoError(t, pendingErr)
	require.Len(t, pendingPage.Images, 2)
	assert.Equal(t, fixture.accepted.ID, pendingPage.Images[0].ID)
	assert.Equal(t, fixture.public.ID, pendingPage.Images[1].ID)
	assert.ErrorIs(t, privateErr, app.ErrImageMarkerForbidden)
}

// https://github.com/openstack/glance/blob/3db7567fd77bad31d5fb2d484c7b8f6ff64eb8ef/glance/api/v2/policy.py#L176-L196
// https://github.com/openstack/glance/blob/3db7567fd77bad31d5fb2d484c7b8f6ff64eb8ef/glance/policies/base.py#L36-L74
func TestOnlyTheOwnerChangesAnImageThatOthersCanSee(t *testing.T) {
	fixture := newVisibilityFixture(t)
	service := fixture.service
	name := "renamed"

	_, sharedUpdateErr := service.Update(memberConsumer, fixture.accepted.ID, app.ImageUpdate{Name: &name})
	_, communityUpdateErr := service.Update(memberStranger, fixture.community.ID, app.ImageUpdate{Name: &name})
	_, privateUpdateErr := service.Update(memberConsumer, fixture.private.ID, app.ImageUpdate{Name: &name})
	sharedDeleteErr := service.Delete(memberConsumer, fixture.accepted.ID)
	privateDeleteErr := service.Delete(memberConsumer, fixture.private.ID)
	sharedUploadErr := service.CheckUpload(memberConsumer, fixture.pending.ID)
	privateUploadErr := service.Upload(memberConsumer, fixture.private.ID, "data")
	publicData, _, publicDownloadErr := service.Download(memberStranger, fixture.public.ID)
	_, privateDownloadErr := service.CheckDownload(memberConsumer, fixture.private.ID)
	unchanged, err := service.Get(memberOwner, fixture.accepted.ID)
	require.NoError(t, err)

	assert.ErrorIs(t, sharedUpdateErr, app.ErrImageForbidden)
	assert.ErrorIs(t, communityUpdateErr, app.ErrImageForbidden)
	assert.ErrorIs(t, privateUpdateErr, app.ErrImageNotFound)
	assert.ErrorIs(t, sharedDeleteErr, app.ErrImageForbidden)
	assert.ErrorIs(t, privateDeleteErr, app.ErrImageNotFound)
	assert.ErrorIs(t, sharedUploadErr, app.ErrImageForbidden)
	assert.ErrorIs(t, privateUploadErr, app.ErrImageNotFound)
	assert.NoError(t, publicDownloadErr)
	assert.Equal(t, "public data", publicData)
	assert.ErrorIs(t, privateDownloadErr, app.ErrImageNotFound)
	assert.Equal(t, fixture.accepted, unchanged)
}

// https://github.com/openstack/glance/blob/3db7567fd77bad31d5fb2d484c7b8f6ff64eb8ef/glance/api/v2/policy.py#L206-L222
func TestOwnerChangesVisibilityExceptToPublic(t *testing.T) {
	fixture := newVisibilityFixture(t)
	service := fixture.service

	unshared, err := service.Update(memberOwner, fixture.accepted.ID, app.ImageUpdate{
		Visibility: pointer(domain.VisibilityPrivate),
	})
	require.NoError(t, err)
	_, hiddenErr := service.Get(memberConsumer, fixture.accepted.ID)
	_, publicErr := service.Update(memberOwner, fixture.private.ID, app.ImageUpdate{
		Visibility: pointer(domain.VisibilityPublic),
	})
	reshared, err := service.Update(memberOwner, fixture.accepted.ID, app.ImageUpdate{
		Visibility: pointer(domain.VisibilityShared),
	})
	require.NoError(t, err)
	membership, err := service.GetMember(memberConsumer, fixture.accepted.ID, memberConsumer)
	require.NoError(t, err)

	assert.Equal(t, domain.VisibilityPrivate, unshared.Visibility)
	assert.True(t, unshared.UpdatedAt.After(fixture.accepted.UpdatedAt))
	assert.ErrorIs(t, hiddenErr, app.ErrImageNotFound)
	assert.ErrorIs(t, publicErr, app.ErrImagePublicize)
	assert.Equal(t, domain.VisibilityShared, reshared.Visibility)
	assert.Equal(t, domain.MemberAccepted, membership.Status)
}
