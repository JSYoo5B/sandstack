package network_test

import (
	"net/netip"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	app "github.com/JSYoo5B/sandstack/internal/app/network"
	domain "github.com/JSYoo5B/sandstack/internal/domain/network"
)

func TestSubnetAddressDefaultsAndDetachedNetworkProjection(t *testing.T) {
	seed := domain.Network{ID: fixtureID(1), ProjectID: "owner"}
	id := 4
	service := app.New(seed, func() string {
		id--
		return fixtureID(id)
	})

	first, err := service.CreateSubnet(
		seed.ProjectID, seed.ID, "first", netip.MustParsePrefix("192.168.0.66/26"), true,
	)
	require.NoError(t, err)
	assert.Equal(t, domain.Subnet{
		ID:              fixtureID(3),
		NetworkID:       seed.ID,
		ProjectID:       seed.ProjectID,
		Name:            "first",
		CIDR:            netip.MustParsePrefix("192.168.0.64/26"),
		EnableDHCP:      true,
		Gateway:         netip.MustParseAddr("192.168.0.65"),
		AllocationStart: netip.MustParseAddr("192.168.0.66"),
		AllocationEnd:   netip.MustParseAddr("192.168.0.126"),
	}, first)
	second, err := service.CreateSubnet(
		seed.ProjectID, seed.ID, "second", netip.MustParsePrefix("192.168.1.253/30"), false,
	)
	require.NoError(t, err)
	assert.Equal(t, netip.MustParseAddr("192.168.1.253"), second.Gateway)
	assert.Equal(t, netip.MustParseAddr("192.168.1.254"), second.AllocationStart)
	assert.Equal(t, second.AllocationStart, second.AllocationEnd)
	assert.False(t, second.EnableDHCP)
	assert.Equal(t, []domain.Subnet{second, first}, service.ListSubnets(seed.ProjectID))

	expectedIDs := []string{second.ID, first.ID}
	parent, err := service.Get(seed.ProjectID, seed.ID)
	require.NoError(t, err)
	listed := service.List(seed.ProjectID, app.ListFilter{})
	require.Len(t, listed, 1)
	require.Equal(t, expectedIDs, parent.SubnetIDs)
	require.Equal(t, expectedIDs, listed[0].SubnetIDs)
	parent.SubnetIDs[0] = "changed"
	listed[0].SubnetIDs[1] = "changed"
	parent, err = service.Get(seed.ProjectID, seed.ID)
	require.NoError(t, err)
	assert.Equal(t, expectedIDs, parent.SubnetIDs)

	require.NoError(t, service.DeleteSubnet(seed.ProjectID, first.ID))
	parent, err = service.Get(seed.ProjectID, seed.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{second.ID}, parent.SubnetIDs)
	require.NoError(t, service.DeleteSubnet(seed.ProjectID, second.ID))
	parent, err = service.Get(seed.ProjectID, seed.ID)
	require.NoError(t, err)
	assert.Nil(t, parent.SubnetIDs)
}

func TestSubnetOwnershipAndNetworkScopedOverlap(t *testing.T) {
	seed := domain.Network{ID: fixtureID(1), ProjectID: "owner"}
	id := 1
	service := app.New(seed, func() string {
		id++
		return fixtureID(id)
	})
	prefix := netip.MustParsePrefix("10.2.0.0/24")
	_, err := service.CreateSubnet("other-project", seed.ID, "foreign", prefix, true)
	assert.ErrorIs(t, err, app.ErrNetworkNotFound)
	_, err = service.CreateSubnet(seed.ProjectID, "missing", "missing", prefix, true)
	assert.ErrorIs(t, err, app.ErrNetworkNotFound)

	subnet, err := service.CreateSubnet(seed.ProjectID, seed.ID, "owned", prefix, true)
	require.NoError(t, err)
	_, err = service.GetSubnet("other-project", subnet.ID)
	assert.ErrorIs(t, err, app.ErrSubnetNotFound)
	assert.ErrorIs(t, service.DeleteSubnet("other-project", subnet.ID), app.ErrSubnetNotFound)
	assert.Empty(t, service.ListSubnets("other-project"))

	for _, overlap := range []string{"10.2.0.4/24", "10.2.0.128/25", "10.2.0.0/23"} {
		_, err = service.CreateSubnet(seed.ProjectID, seed.ID, "overlap", netip.MustParsePrefix(overlap), true)
		assert.ErrorIs(t, err, app.ErrSubnetOverlap)
	}
	for _, invalid := range []string{"0.0.0.0/0", "10.0.0.0/31", "10.0.0.1/32", "2001:db8::/24"} {
		_, err = service.CreateSubnet(seed.ProjectID, seed.ID, "invalid", netip.MustParsePrefix(invalid), true)
		assert.ErrorIs(t, err, app.ErrSubnetCIDR)
	}
	assert.Equal(t, []domain.Subnet{subnet}, service.ListSubnets(seed.ProjectID))

	other, err := service.Create(seed.ProjectID, "another network", false)
	require.NoError(t, err)
	_, err = service.CreateSubnet(seed.ProjectID, other.ID, "overlap on another network", prefix, true)
	assert.NoError(t, err)
}

func TestNetworkDeletionAndResetRemoveChildSubnets(t *testing.T) {
	seed := domain.Network{ID: fixtureID(1), ProjectID: "owner"}
	ids := []string{seed.ID, fixtureID(2), fixtureID(2), fixtureID(3), fixtureID(4)}
	service := app.New(seed, func() string {
		id := ids[0]
		ids = ids[1:]
		return id
	})
	prefix := netip.MustParsePrefix("10.2.0.0/24")
	seedSubnet, err := service.CreateSubnet(seed.ProjectID, seed.ID, "seed subnet", prefix, true)
	require.NoError(t, err)
	assert.Equal(t, fixtureID(2), seedSubnet.ID)
	other, err := service.Create(seed.ProjectID, "another network", false)
	require.NoError(t, err)
	assert.Equal(t, fixtureID(3), other.ID)
	otherSubnet, err := service.CreateSubnet(seed.ProjectID, other.ID, "other subnet", prefix, true)
	require.NoError(t, err)

	require.NoError(t, service.Delete(seed.ProjectID, seed.ID))
	_, err = service.GetSubnet(seed.ProjectID, seedSubnet.ID)
	assert.ErrorIs(t, err, app.ErrSubnetNotFound)
	assert.Equal(t, []domain.Subnet{otherSubnet}, service.ListSubnets(seed.ProjectID))

	service.Reset()
	assert.Equal(t, []domain.Network{seed}, service.List(seed.ProjectID, app.ListFilter{}))
	assert.Empty(t, service.ListSubnets(seed.ProjectID))
	_, err = service.GetSubnet(seed.ProjectID, otherSubnet.ID)
	assert.ErrorIs(t, err, app.ErrSubnetNotFound)
}
