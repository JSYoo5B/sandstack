package network

import (
	"errors"
	"net/netip"
	"net/url"

	app "github.com/JSYoo5B/sandstack/internal/app/network"
)

var errSubnetListCIDR = errors.New("the subnet list CIDR filter requires an address with a prefix length")
var errSubnetListDHCP = errors.New("the subnet list enable_dhcp filter requires a boolean")

func parseSubnetListQuery(query url.Values) (app.SubnetListFilter, error) {
	for field := range query {
		switch field {
		case "id", "name", "network_id", "cidr", "gateway_ip", "enable_dhcp", "limit", "marker", "page_reverse":
		default:
			return app.SubnetListFilter{}, errors.New("the subnet list query contains an unsupported parameter")
		}
	}
	filter := app.SubnetListFilter{
		IDs:        query["id"],
		Names:      query["name"],
		NetworkIDs: query["network_id"],
		GatewayIPs: query["gateway_ip"],
	}
	for _, value := range query["cidr"] {
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			return app.SubnetListFilter{}, errSubnetListCIDR
		}
		filter.CIDRs = append(filter.CIDRs, prefix)
	}
	dhcpStates, ok := parseBooleanFilters(query["enable_dhcp"])
	if !ok {
		return app.SubnetListFilter{}, errSubnetListDHCP
	}
	filter.DHCPStates = dhcpStates
	return filter, nil
}
