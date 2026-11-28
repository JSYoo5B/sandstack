package server_test

import (
	"net/http"
	"testing"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/attributestags"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/layer3/floatingips"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/layer3/routers"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/security/groups"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/networks"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/ports"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/subnets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGophercloudTagsNeutronNetworksAndPorts(t *testing.T) {
	instance := startServer(t, "")
	resources := createRouterInterfaceResources(t, authenticate(t, instance))
	client := resources.service
	ctx := t.Context()
	network := resources.networkID
	port, err := ports.Create(ctx, client, ports.CreateOpts{NetworkID: network}).Extract()
	require.NoError(t, err)

	// https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/extensions/tagging.py#L206-L323
	replaced, err := attributestags.ReplaceAll(ctx, client, "networks", network, attributestags.ReplaceAllOpts{
		Tags: []string{"red", "blue"},
	}).Extract()
	require.NoError(t, err)
	listed, err := attributestags.List(ctx, client, "networks", network).Extract()
	require.NoError(t, err)
	require.NoError(t, attributestags.Add(ctx, client, "networks", network, "green").ExtractErr())
	require.NoError(t, attributestags.Add(ctx, client, "networks", network, "red").ExtractErr())
	present, err := attributestags.Confirm(ctx, client, "networks", network, "green").Extract()
	require.NoError(t, err)
	absent, err := attributestags.Confirm(ctx, client, "networks", network, "purple").Extract()
	require.NoError(t, err)
	require.NoError(t, attributestags.Delete(ctx, client, "networks", network, "blue").ExtractErr())
	repeatDelete := attributestags.Delete(ctx, client, "networks", network, "blue").ExtractErr()
	shown, err := networks.Get(ctx, client, network).Extract()
	require.NoError(t, err)

	assert.Equal(t, []string{"red", "blue"}, replaced)
	assert.Equal(t, []string{"blue", "red"}, listed)
	assert.True(t, present)
	assert.False(t, absent)
	assert.True(t, gophercloud.ResponseCodeIs(repeatDelete, http.StatusNotFound), "%v", repeatDelete)
	assert.Equal(t, []string{"green", "red"}, shown.Tags)

	// https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/services/tag/tag_plugin.py#L46-L54
	_, err = attributestags.ReplaceAll(ctx, client, "ports", port.ID, attributestags.ReplaceAllOpts{
		Tags: []string{"web", "frontend"},
	}).Extract()
	require.NoError(t, err)
	require.NoError(t, attributestags.Add(ctx, client, "ports", port.ID, "public").ExtractErr())
	shownPort, err := ports.Get(ctx, client, port.ID).Extract()
	require.NoError(t, err)
	require.NoError(t, attributestags.DeleteAll(ctx, client, "networks", network).ExtractErr())
	cleared, err := attributestags.List(ctx, client, "networks", network).Extract()
	require.NoError(t, err)
	clearedNetwork, err := networks.Get(ctx, client, network).Extract()
	require.NoError(t, err)

	assert.Equal(t, []string{"frontend", "public", "web"}, shownPort.Tags)
	assert.Equal(t, []string{}, cleared)
	assert.Equal(t, []string{}, clearedNetwork.Tags)
}

func TestGophercloudFiltersNeutronListsByTags(t *testing.T) {
	instance := startServer(t, "")
	resources := createFloatingIPResources(t, authenticate(t, instance))
	client := resources.service
	ctx := t.Context()
	tagSets := [][]string{{"red"}, {"red", "blue"}, {"red", "blue", "green"}, {"green"}, {}}
	networkIDs := make([]string, len(tagSets))
	portIDs := make([]string, len(tagSets))
	for i, tags := range tagSets {
		network, err := networks.Create(ctx, client, networks.CreateOpts{Name: "tagged"}).Extract()
		require.NoError(t, err)
		port, err := ports.Create(ctx, client, ports.CreateOpts{NetworkID: network.ID, Name: "tagged"}).Extract()
		require.NoError(t, err)
		for kind, id := range map[string]string{"networks": network.ID, "ports": port.ID} {
			_, err := attributestags.ReplaceAll(ctx, client, kind, id, attributestags.ReplaceAllOpts{Tags: tags}).Extract()
			require.NoError(t, err)
		}
		networkIDs[i], portIDs[i] = network.ID, port.ID
	}
	pick := func(ids []string, indexes ...int) []string {
		picked := make([]string, len(indexes))
		for i, index := range indexes {
			picked[i] = ids[index]
		}
		return picked
	}
	listNetworks := func(opts networks.ListOpts) []string {
		t.Helper()
		opts.Name = "tagged"
		pages, err := networks.List(client, opts).AllPages(ctx)
		require.NoError(t, err)
		listed, err := networks.ExtractNetworks(pages)
		require.NoError(t, err)
		ids := make([]string, len(listed))
		for i, network := range listed {
			ids[i] = network.ID
		}
		return ids
	}
	listPorts := func(opts ports.ListOpts) []string {
		t.Helper()
		opts.Name = "tagged"
		pages, err := ports.List(client, opts).AllPages(ctx)
		require.NoError(t, err)
		listed, err := ports.ExtractPorts(pages)
		require.NoError(t, err)
		ids := make([]string, len(listed))
		for i, port := range listed {
			ids[i] = port.ID
		}
		return ids
	}

	// https://github.com/openstack/neutron/blob/ba84fbf374cc7f5055eb2ff4ebc34d2a19cbbc3d/neutron/objects/tag.py#L30-L103
	// https://github.com/openstack/neutron-tempest-plugin/blob/fb16056011dd71d4d0e768e9f61fc7aa2f4317c0/neutron_tempest_plugin/api/admin/test_tag.py#L256-L295
	assert.ElementsMatch(t, pick(networkIDs, 0, 1, 2), listNetworks(networks.ListOpts{Tags: "red"}))
	assert.ElementsMatch(t, pick(networkIDs, 1, 2), listNetworks(networks.ListOpts{Tags: "red,blue"}))
	assert.ElementsMatch(t, pick(networkIDs, 1, 2), listNetworks(networks.ListOpts{TagsAny: "blue"}))
	assert.ElementsMatch(t, pick(networkIDs, 0, 1, 2), listNetworks(networks.ListOpts{TagsAny: "red,blue"}))
	assert.ElementsMatch(t, pick(networkIDs, 3, 4), listNetworks(networks.ListOpts{NotTags: "red"}))
	assert.ElementsMatch(t, pick(networkIDs, 0, 3, 4), listNetworks(networks.ListOpts{NotTags: "red,blue"}))
	assert.ElementsMatch(t, pick(networkIDs, 0, 3, 4), listNetworks(networks.ListOpts{NotTagsAny: "blue"}))
	assert.ElementsMatch(t, pick(networkIDs, 3, 4), listNetworks(networks.ListOpts{NotTagsAny: "red,blue"}))
	assert.ElementsMatch(t, pick(portIDs, 1, 2), listPorts(ports.ListOpts{Tags: "red,blue"}))
	assert.ElementsMatch(t, pick(portIDs, 2, 3), listPorts(ports.ListOpts{TagsAny: "green"}))
	assert.ElementsMatch(t, pick(portIDs, 0, 3, 4), listPorts(ports.ListOpts{NotTags: "red,blue"}))
	assert.ElementsMatch(t, pick(portIDs, 3, 4), listPorts(ports.ListOpts{NotTagsAny: "red"}))
	assert.ElementsMatch(t, pick(portIDs, 2), listPorts(ports.ListOpts{Tags: "red", TagsAny: "green"}))
}

func TestGophercloudTagsOtherNeutronResources(t *testing.T) {
	instance := startServer(t, "")
	resources := createFloatingIPResources(t, authenticate(t, instance))
	client := resources.service
	ctx := t.Context()
	group, err := groups.Create(ctx, client, groups.CreateOpts{Name: "tagged"}).Extract()
	require.NoError(t, err)
	floatingIP, err := floatingips.Create(ctx, client, floatingips.CreateOpts{
		FloatingNetworkID: resources.externalNetworkID,
	}).Extract()
	require.NoError(t, err)
	tags := []string{"blue", "red"}
	for _, tc := range []struct {
		kind   string
		id     string
		listed func(string) ([][]string, error)
	}{
		{"subnets", resources.subnetID, func(filter string) ([][]string, error) {
			pages, err := subnets.List(client, subnets.ListOpts{Tags: filter}).AllPages(ctx)
			if err != nil {
				return nil, err
			}
			listed, err := subnets.ExtractSubnets(pages)
			return collectTags(listed, err, func(subnet subnets.Subnet) []string { return subnet.Tags })
		}},
		{"routers", resources.routerID, func(filter string) ([][]string, error) {
			pages, err := routers.List(client, routers.ListOpts{Tags: filter}).AllPages(ctx)
			if err != nil {
				return nil, err
			}
			listed, err := routers.ExtractRouters(pages)
			return collectTags(listed, err, func(router routers.Router) []string { return router.Tags })
		}},
		{"security-groups", group.ID, func(filter string) ([][]string, error) {
			pages, err := groups.List(client, groups.ListOpts{Tags: filter}).AllPages(ctx)
			if err != nil {
				return nil, err
			}
			listed, err := groups.ExtractGroups(pages)
			return collectTags(listed, err, func(group groups.SecGroup) []string { return group.Tags })
		}},
		{"floatingips", floatingIP.ID, func(filter string) ([][]string, error) {
			pages, err := floatingips.List(client, floatingips.ListOpts{Tags: filter}).AllPages(ctx)
			if err != nil {
				return nil, err
			}
			listed, err := floatingips.ExtractFloatingIPs(pages)
			return collectTags(listed, err, func(floatingIP floatingips.FloatingIP) []string { return floatingIP.Tags })
		}},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			replaced, err := attributestags.ReplaceAll(ctx, client, tc.kind, tc.id, attributestags.ReplaceAllOpts{
				Tags: []string{"red", "blue"},
			}).Extract()
			require.NoError(t, err)
			matched, err := tc.listed("blue,red")
			require.NoError(t, err)
			unmatched, err := tc.listed("blue,green")
			require.NoError(t, err)
			require.NoError(t, attributestags.DeleteAll(ctx, client, tc.kind, tc.id).ExtractErr())
			cleared, err := attributestags.List(ctx, client, tc.kind, tc.id).Extract()
			require.NoError(t, err)

			assert.Equal(t, []string{"red", "blue"}, replaced)
			assert.Equal(t, [][]string{tags}, matched)
			assert.Empty(t, unmatched)
			assert.Equal(t, []string{}, cleared)
		})
	}
}

func collectTags[T any](items []T, err error, tags func(T) []string) ([][]string, error) {
	if err != nil {
		return nil, err
	}
	collected := make([][]string, len(items))
	for i, item := range items {
		collected[i] = tags(item)
	}
	return collected, nil
}
