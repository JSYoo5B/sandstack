package network_test

import (
	"net/netip"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	app "github.com/JSYoo5B/sandstack/internal/app/network"
	domain "github.com/JSYoo5B/sandstack/internal/domain/network"
)

type floatingIPFixture struct {
	service  *app.Service
	seed     domain.Network
	external domain.Network
	subnet   domain.Subnet
	router   domain.Router
	port     domain.Port
}

func newFloatingIPFixture(t *testing.T, externalCIDR string) floatingIPFixture {
	t.Helper()
	seed, external, _, service := newExternalNetworkFixture(externalCIDR)
	subnet, err := service.CreateSubnet(seed.ProjectID, seed.ID, app.SubnetCreate{
		CIDR: netip.MustParsePrefix("10.0.0.0/24"),
	})
	require.NoError(t, err)
	router, err := service.CreateRouterWithGateway(seed.ProjectID, "edge", "", true,
		app.RouterGatewayUpdate{NetworkID: external.ID})
	require.NoError(t, err)
	_, err = service.AddRouterInterface(seed.ProjectID, router.ID, subnet.ID)
	require.NoError(t, err)
	port, err := service.CreatePort(seed.ProjectID, seed.ID, "server", false, nil)
	require.NoError(t, err)
	return floatingIPFixture{
		service: service, seed: seed, external: external, subnet: subnet, router: router, port: port,
	}
}

func TestFloatingIPAssociationConvergesAfterEachResponse(t *testing.T) {
	f := newFloatingIPFixture(t, "203.0.113.0/29")
	projectID := f.seed.ProjectID

	created, err := f.service.CreateFloatingIP(projectID, app.FloatingIPCreate{
		NetworkID: f.external.ID, Description: "web",
	})
	require.NoError(t, err)
	unassociated, err := f.service.GetFloatingIP(projectID, created.ID)
	require.NoError(t, err)

	assert.Equal(t, domain.FloatingIP{
		ID:                created.ID,
		ProjectID:         projectID,
		Description:       "web",
		FloatingNetworkID: f.external.ID,
		FloatingAddress:   netip.MustParseAddr("203.0.113.3"),
		FloatingPortID:    created.FloatingPortID,
		Status:            domain.FloatingIPStatusDown,
	}, created)
	assert.Equal(t, created, unassociated)
	_, err = f.service.GetPort(projectID, created.FloatingPortID)
	assert.ErrorIs(t, err, app.ErrPortNotFound)

	associated, err := f.service.UpdateFloatingIP(projectID, created.ID, app.FloatingIPUpdate{PortID: &f.port.ID})
	require.NoError(t, err)
	active, err := f.service.GetFloatingIP(projectID, created.ID)
	require.NoError(t, err)

	assert.Equal(t, domain.FloatingIPStatusDown, associated.Status)
	assert.Equal(t, f.port.ID, associated.PortID)
	assert.Equal(t, f.port.FixedIP, associated.FixedIP)
	assert.Equal(t, f.router.ID, associated.RouterID)
	assert.Equal(t, domain.FloatingIPStatusActive, active.Status)
	description := "renamed"
	renamed, err := f.service.UpdateFloatingIP(projectID, created.ID, app.FloatingIPUpdate{Description: &description})
	require.NoError(t, err)
	assert.Equal(t, f.port.ID, renamed.PortID)
	assert.Equal(t, domain.FloatingIPStatusActive, renamed.Status)

	empty := ""
	disassociated, err := f.service.UpdateFloatingIP(projectID, created.ID, app.FloatingIPUpdate{PortID: &empty})
	require.NoError(t, err)
	down, err := f.service.GetFloatingIP(projectID, created.ID)
	require.NoError(t, err)

	assert.Equal(t, domain.FloatingIPStatusActive, disassociated.Status)
	assert.Empty(t, disassociated.PortID)
	assert.False(t, disassociated.FixedIP.IsValid())
	assert.Empty(t, disassociated.RouterID)
	assert.Equal(t, domain.FloatingIPStatusDown, down.Status)

	direct, err := f.service.CreateFloatingIP(projectID, app.FloatingIPCreate{
		NetworkID: f.external.ID, PortID: f.port.ID, FixedIP: f.port.FixedIP,
	})
	require.NoError(t, err)
	listed := f.service.ListFloatingIPs(projectID, app.FloatingIPListFilter{PortIDs: []string{f.port.ID}})
	require.Len(t, listed, 1)

	assert.Equal(t, domain.FloatingIPStatusDown, direct.Status)
	assert.Equal(t, netip.MustParseAddr("203.0.113.4"), direct.FloatingAddress)
	assert.Equal(t, direct.ID, listed[0].ID)
	assert.Equal(t, domain.FloatingIPStatusActive, listed[0].Status)
	assert.Empty(t, f.service.ListFloatingIPs("other-project", app.FloatingIPListFilter{}))
	_, err = f.service.GetFloatingIP("other-project", direct.ID)
	assert.ErrorIs(t, err, app.ErrFloatingIPNotFound)

	require.NoError(t, f.service.DeleteFloatingIP(projectID, created.ID))
	reused, err := f.service.CreateFloatingIP(projectID, app.FloatingIPCreate{NetworkID: f.external.ID})
	require.NoError(t, err)
	assert.Equal(t, created.FloatingAddress, reused.FloatingAddress)
	assert.ErrorIs(t, f.service.DeleteFloatingIP(projectID, created.ID), app.ErrFloatingIPNotFound)
}

func TestFloatingIPRejectsUnusableTargetsWithoutAllocating(t *testing.T) {
	f := newFloatingIPFixture(t, "203.0.113.0/29")
	projectID := f.seed.ProjectID
	empty := ""
	unaddressed, err := f.service.CreatePort(projectID, f.seed.ID, "unaddressed", false, &empty)
	require.NoError(t, err)
	isolated, err := f.service.Create(projectID, "isolated", false)
	require.NoError(t, err)
	_, err = f.service.CreateSubnet(projectID, isolated.ID, app.SubnetCreate{CIDR: netip.MustParsePrefix("10.1.0.0/24")})
	require.NoError(t, err)
	unreachable, err := f.service.CreatePort(projectID, isolated.ID, "", false, nil)
	require.NoError(t, err)
	foreignNetwork, err := f.service.Create("other-project", "", false)
	require.NoError(t, err)
	foreign, err := f.service.CreatePort("other-project", foreignNetwork.ID, "", false, nil)
	require.NoError(t, err)
	first, err := f.service.CreateFloatingIP(projectID, app.FloatingIPCreate{NetworkID: f.external.ID, PortID: f.port.ID})
	require.NoError(t, err)

	for _, tc := range []struct {
		name    string
		request app.FloatingIPCreate
		want    error
	}{
		{name: "missing network", request: app.FloatingIPCreate{NetworkID: fixtureID(99)}, want: app.ErrNetworkNotFound},
		{name: "private network", request: app.FloatingIPCreate{NetworkID: f.seed.ID}, want: app.ErrNetworkNotExternal},
		{
			name:    "fixed IP without port",
			request: app.FloatingIPCreate{NetworkID: f.external.ID, FixedIP: f.port.FixedIP},
			want:    app.ErrFloatingIPFixedIPWithoutPort,
		},
		{
			name:    "missing port",
			request: app.FloatingIPCreate{NetworkID: f.external.ID, PortID: fixtureID(99)},
			want:    app.ErrPortNotFound,
		},
		{
			name:    "foreign port",
			request: app.FloatingIPCreate{NetworkID: f.external.ID, PortID: foreign.ID},
			want:    app.ErrPortNotFound,
		},
		{
			name:    "port without address",
			request: app.FloatingIPCreate{NetworkID: f.external.ID, PortID: unaddressed.ID},
			want:    app.ErrFloatingIPPortNoIPv4,
		},
		{
			name: "address not on port",
			request: app.FloatingIPCreate{
				NetworkID: f.external.ID, PortID: f.port.ID, FixedIP: netip.MustParseAddr("10.0.0.99"),
			},
			want: app.ErrFloatingIPFixedIPNotOnPort,
		},
		{
			name:    "unreachable subnet",
			request: app.FloatingIPCreate{NetworkID: f.external.ID, PortID: unreachable.ID},
			want:    app.ErrExternalGatewayForFloatingIPNotFound,
		},
		{
			name:    "fixed IP already associated",
			request: app.FloatingIPCreate{NetworkID: f.external.ID, PortID: f.port.ID},
			want:    app.ErrFloatingIPPortAlreadyAssociated,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := f.service.CreateFloatingIP(projectID, tc.request)

			assert.ErrorIs(t, err, tc.want)
		})
	}
	second, err := f.service.CreateFloatingIP(projectID, app.FloatingIPCreate{NetworkID: f.external.ID})
	require.NoError(t, err)
	assert.Equal(t, first.FloatingAddress.Next(), second.FloatingAddress)
	assert.Len(t, f.service.ListFloatingIPs(projectID, app.FloatingIPListFilter{}), 2)

	_, err = f.service.UpdateFloatingIP(projectID, second.ID, app.FloatingIPUpdate{PortID: &f.port.ID})
	assert.ErrorIs(t, err, app.ErrFloatingIPPortAlreadyAssociated)
	_, err = f.service.UpdateFloatingIP(projectID, second.ID, app.FloatingIPUpdate{FixedIP: f.port.FixedIP})
	assert.ErrorIs(t, err, app.ErrFloatingIPFixedIPWithoutPort)
	_, err = f.service.UpdateFloatingIP(projectID, fixtureID(99), app.FloatingIPUpdate{})
	assert.ErrorIs(t, err, app.ErrFloatingIPNotFound)
	unchanged, err := f.service.GetFloatingIP(projectID, second.ID)
	require.NoError(t, err)
	assert.Equal(t, second, unchanged)
}

func TestFloatingIPKeepsItsRouterPathInUse(t *testing.T) {
	f := newFloatingIPFixture(t, "203.0.113.0/29")
	projectID := f.seed.ProjectID
	floatingIP, err := f.service.CreateFloatingIP(projectID, app.FloatingIPCreate{
		NetworkID: f.external.ID, PortID: f.port.ID,
	})
	require.NoError(t, err)
	clear := app.RouterGatewayUpdate{}
	moved := netip.MustParseAddr("10.0.0.50")

	_, gatewayErr := f.service.UpdateRouter(projectID, f.router.ID, app.RouterUpdate{Gateway: &clear})
	_, interfaceErr := f.service.RemoveRouterInterface(projectID, f.router.ID, f.subnet.ID)
	_, addressErr := f.service.UpdatePort(projectID, f.port.ID, app.PortUpdate{
		FixedIPs: &app.PortFixedIPUpdate{Address: moved},
	})
	renamed := "renamed"
	_, renameErr := f.service.UpdatePort(projectID, f.port.ID, app.PortUpdate{Name: &renamed})

	assert.ErrorIs(t, gatewayErr, app.ErrRouterGatewayInUseByFloatingIP)
	assert.ErrorIs(t, interfaceErr, app.ErrRouterInterfaceInUseByFloatingIP)
	assert.ErrorIs(t, addressErr, app.ErrPortFixedIPHasFloatingIP)
	assert.NoError(t, renameErr)
	router, err := f.service.GetRouter(projectID, f.router.ID)
	require.NoError(t, err)
	assert.Equal(t, f.router.Gateway, router.Gateway)

	require.NoError(t, f.service.DeletePort(projectID, f.port.ID))
	released, err := f.service.GetFloatingIP(projectID, floatingIP.ID)
	require.NoError(t, err)

	assert.Empty(t, released.PortID)
	assert.False(t, released.FixedIP.IsValid())
	assert.Empty(t, released.RouterID)
	assert.Equal(t, domain.FloatingIPStatusDown, released.Status)
	_, err = f.service.RemoveRouterInterface(projectID, f.router.ID, f.subnet.ID)
	assert.NoError(t, err)
	_, err = f.service.UpdateRouter(projectID, f.router.ID, app.RouterUpdate{Gateway: &clear})
	assert.NoError(t, err)
}

func TestFloatingIPInterfaceRemovalByPortIsBlocked(t *testing.T) {
	f := newFloatingIPFixture(t, "203.0.113.0/29")
	projectID := f.seed.ProjectID
	ports := f.service.ListPorts(projectID, app.PortListFilter{DeviceIDs: []string{f.router.ID}})
	require.Len(t, ports, 1)
	_, err := f.service.CreateFloatingIP(projectID, app.FloatingIPCreate{NetworkID: f.external.ID, PortID: f.port.ID})
	require.NoError(t, err)

	_, err = f.service.RemoveRouterInterfaceByPort(projectID, f.router.ID, ports[0].ID, "")

	assert.ErrorIs(t, err, app.ErrRouterInterfaceInUseByFloatingIP)
}

func TestFloatingIPPrefersRouterOwningSubnetGateway(t *testing.T) {
	f := newFloatingIPFixture(t, "203.0.113.0/28")
	projectID := f.seed.ProjectID
	_, err := f.service.RemoveRouterInterface(projectID, f.router.ID, f.subnet.ID)
	require.NoError(t, err)
	address := netip.MustParseAddr("10.0.0.254")
	routerPort, err := f.service.CreatePortWithFixedIP(projectID, f.seed.ID, "", false, nil, address)
	require.NoError(t, err)
	_, err = f.service.AddRouterInterfaceByPort(projectID, f.router.ID, routerPort.ID)
	require.NoError(t, err)
	gatewayOwner, err := f.service.CreateRouterWithGateway(projectID, "gateway owner", "", true,
		app.RouterGatewayUpdate{NetworkID: f.external.ID})
	require.NoError(t, err)
	_, err = f.service.AddRouterInterface(projectID, gatewayOwner.ID, f.subnet.ID)
	require.NoError(t, err)
	require.Less(t, f.router.ID, gatewayOwner.ID)

	floatingIP, err := f.service.CreateFloatingIP(projectID, app.FloatingIPCreate{
		NetworkID: f.external.ID, PortID: f.port.ID,
	})
	require.NoError(t, err)

	assert.Equal(t, gatewayOwner.ID, floatingIP.RouterID)
	_, err = f.service.RemoveRouterInterface(projectID, gatewayOwner.ID, f.subnet.ID)
	assert.ErrorIs(t, err, app.ErrRouterInterfaceInUseByFloatingIP)
	_, err = f.service.RemoveRouterInterfaceByPort(projectID, f.router.ID, routerPort.ID, "")
	assert.NoError(t, err)
}

func TestFloatingIPSharesExternalPoolAndResets(t *testing.T) {
	f := newFloatingIPFixture(t, "203.0.113.0/30")
	projectID := f.seed.ProjectID

	_, err := f.service.CreateFloatingIP(projectID, app.FloatingIPCreate{NetworkID: f.external.ID})

	assert.ErrorIs(t, err, app.ErrPortIPExhausted)
	assert.Empty(t, f.service.ListFloatingIPs(projectID, app.FloatingIPListFilter{}))

	empty := ""
	_, err = f.service.UpdateRouter(projectID, f.router.ID, app.RouterUpdate{Gateway: &app.RouterGatewayUpdate{NetworkID: empty}})
	require.NoError(t, err)
	created, err := f.service.CreateFloatingIP(projectID, app.FloatingIPCreate{NetworkID: f.external.ID})
	require.NoError(t, err)

	f.service.Reset()

	assert.Empty(t, f.service.ListFloatingIPs(projectID, app.FloatingIPListFilter{}))
	_, err = f.service.GetFloatingIP(projectID, created.ID)
	assert.ErrorIs(t, err, app.ErrFloatingIPNotFound)
	recreated, err := f.service.CreateFloatingIP(projectID, app.FloatingIPCreate{NetworkID: f.external.ID})
	require.NoError(t, err)
	assert.Equal(t, created.FloatingAddress, recreated.FloatingAddress)
}
