package network_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	app "github.com/JSYoo5B/sandstack/internal/app/network"
	domain "github.com/JSYoo5B/sandstack/internal/domain/network"
)

func TestResourceTagsReplaceAddRemoveAndClear(t *testing.T) {
	seed, _, _, service := newExternalNetworkFixture("203.0.113.0/24")
	projectID := seed.ProjectID
	name := "renamed"

	require.NoError(t, service.ReplaceResourceTags(projectID, app.TagNetwork, seed.ID, []string{"red", "blue"}))
	replaced, err := service.ResourceTags(projectID, app.TagNetwork, seed.ID)
	require.NoError(t, err)
	require.NoError(t, service.AddResourceTag(projectID, app.TagNetwork, seed.ID, "green"))
	require.NoError(t, service.AddResourceTag(projectID, app.TagNetwork, seed.ID, "red"))
	_, err = service.Update(projectID, seed.ID, app.NetworkUpdate{Name: &name})
	require.NoError(t, err)
	updated, err := service.Get(projectID, seed.ID)
	require.NoError(t, err)
	present := service.CheckResourceTag(projectID, app.TagNetwork, seed.ID, "green")
	absent := service.CheckResourceTag(projectID, app.TagNetwork, seed.ID, "purple")
	removeAbsent := service.RemoveResourceTag(projectID, app.TagNetwork, seed.ID, "purple")
	require.NoError(t, service.RemoveResourceTag(projectID, app.TagNetwork, seed.ID, "blue"))
	removed, err := service.ResourceTags(projectID, app.TagNetwork, seed.ID)
	require.NoError(t, err)
	require.NoError(t, service.ClearResourceTags(projectID, app.TagNetwork, seed.ID))
	cleared, err := service.Get(projectID, seed.ID)
	require.NoError(t, err)

	assert.Equal(t, []string{"blue", "red"}, replaced)
	assert.Equal(t, []string{"blue", "green", "red"}, updated.Tags)
	assert.Equal(t, name, updated.Name)
	assert.NoError(t, present)
	assert.ErrorIs(t, absent, app.ErrTagNotFound)
	assert.ErrorIs(t, removeAbsent, app.ErrTagNotFound)
	assert.Equal(t, []string{"green", "red"}, removed)
	assert.Nil(t, cleared.Tags)
}

func TestResourceTagsFollowReadAndWriteAccess(t *testing.T) {
	seed, external, subnet, service := newExternalNetworkFixture("203.0.113.0/24")
	projectID := seed.ProjectID
	router, err := service.CreateRouterWithGateway(
		projectID, "edge", "", true, app.RouterGatewayUpdate{NetworkID: external.ID},
	)
	require.NoError(t, err)
	require.NotNil(t, router.Gateway)

	externalTags, externalErr := service.ResourceTags(projectID, app.TagNetwork, external.ID)
	subnetTags, subnetErr := service.ResourceTags(projectID, app.TagSubnet, subnet.ID)
	_, gatewayPortErr := service.ResourceTags(projectID, app.TagPort, router.Gateway.PortID)
	_, foreignRouterErr := service.ResourceTags("foreign", app.TagRouter, router.ID)
	_, missingErr := service.ResourceTags(projectID, app.TagRouter, fixtureID(99))
	replaceExternalErr := service.ReplaceResourceTags(projectID, app.TagNetwork, external.ID, []string{"mine"})
	addSubnetErr := service.AddResourceTag(projectID, app.TagSubnet, subnet.ID, "mine")
	clearMissingErr := service.ClearResourceTags(projectID, app.TagFloatingIP, fixtureID(99))

	require.NoError(t, externalErr)
	require.NoError(t, subnetErr)
	assert.Empty(t, externalTags)
	assert.Empty(t, subnetTags)
	assert.ErrorIs(t, gatewayPortErr, app.ErrTagResourceForbidden)
	assert.ErrorIs(t, foreignRouterErr, app.ErrTagResourceForbidden)
	assert.ErrorIs(t, missingErr, app.ErrTagResourceNotFound)
	assert.ErrorIs(t, replaceExternalErr, app.ErrTagResourceForbidden)
	assert.ErrorIs(t, addSubnetErr, app.ErrTagResourceForbidden)
	assert.ErrorIs(t, clearMissingErr, app.ErrTagResourceNotFound)
}

func TestResourceTagsLimitCountsDistinctTags(t *testing.T) {
	seed := domain.Network{ID: fixtureID(0), ProjectID: "owner"}
	service := app.New(seed, nil)
	groups := service.ListSecurityGroups(seed.ProjectID, app.SecurityGroupFilter{})
	require.Len(t, groups, 1)
	groupID := groups[0].ID
	tags := make([]string, app.MaxResourceTags+1)
	for i := range tags {
		tags[i] = fmt.Sprintf("tag-%02d", i)
	}
	full := tags[:app.MaxResourceTags]

	overReplace := service.ReplaceResourceTags(seed.ProjectID, app.TagSecurityGroup, groupID, tags)
	require.NoError(t, service.ReplaceResourceTags(seed.ProjectID, app.TagSecurityGroup, groupID, full))
	existingAdd := service.AddResourceTag(seed.ProjectID, app.TagSecurityGroup, groupID, full[0])
	overAdd := service.AddResourceTag(seed.ProjectID, app.TagSecurityGroup, groupID, tags[app.MaxResourceTags])
	stored, err := service.GetSecurityGroup(seed.ProjectID, groupID)
	require.NoError(t, err)

	assert.ErrorIs(t, overReplace, app.ErrTagLimitExceeded)
	assert.NoError(t, existingAdd)
	assert.ErrorIs(t, overAdd, app.ErrTagLimitExceeded)
	assert.Equal(t, full, stored.Tags)
}

func TestTagFiltersCombineAllAnyAndNegatedMatches(t *testing.T) {
	seed := domain.Network{ID: fixtureID(0), ProjectID: "owner"}
	sequence := 0
	service := app.New(seed, func() string {
		sequence++
		return fixtureID(sequence)
	})
	tagSets := [][]string{{"red"}, {"red", "blue"}, {"red", "blue", "green"}, {"green"}, nil}
	routerIDs := make([]string, len(tagSets))
	for i, tags := range tagSets {
		router, err := service.CreateRouter(seed.ProjectID, fmt.Sprintf("router-%d", i), "", true)
		require.NoError(t, err)
		require.NoError(t, service.ReplaceResourceTags(seed.ProjectID, app.TagRouter, router.ID, tags))
		routerIDs[i] = router.ID
	}
	listed := func(filter app.TagFilter) []string {
		routers := service.ListRouters(seed.ProjectID, app.RouterListFilter{Tags: filter})
		ids := make([]string, len(routers))
		for i, router := range routers {
			ids[i] = router.ID
		}
		return ids
	}
	pick := func(indexes ...int) []string {
		ids := make([]string, len(indexes))
		for i, index := range indexes {
			ids[i] = routerIDs[index]
		}
		return ids
	}

	// https://github.com/openstack/neutron-tempest-plugin/blob/fb16056011dd71d4d0e768e9f61fc7aa2f4317c0/neutron_tempest_plugin/api/admin/test_tag.py#L256-L295
	assert.Equal(t, pick(0, 1, 2), listed(app.TagFilter{All: []string{"red"}}))
	assert.Equal(t, pick(1, 2), listed(app.TagFilter{All: []string{"red", "blue"}}))
	assert.Equal(t, pick(1, 2), listed(app.TagFilter{Any: []string{"blue"}}))
	assert.Equal(t, pick(0, 1, 2), listed(app.TagFilter{Any: []string{"red", "blue"}}))
	assert.Equal(t, pick(3, 4), listed(app.TagFilter{NotAll: []string{"red"}}))
	assert.Equal(t, pick(0, 3, 4), listed(app.TagFilter{NotAll: []string{"red", "blue"}}))
	assert.Equal(t, pick(0, 3, 4), listed(app.TagFilter{NotAny: []string{"blue"}}))
	assert.Equal(t, pick(3, 4), listed(app.TagFilter{NotAny: []string{"red", "blue"}}))
	assert.Equal(t, pick(2), listed(app.TagFilter{All: []string{"red"}, Any: []string{"green"}}))
	assert.Equal(t, pick(3), listed(app.TagFilter{All: []string{"green"}, NotAny: []string{"blue"}}))
}
