package network

import (
	"errors"
	"net/netip"
	"net/url"

	app "github.com/JSYoo5B/sandstack/internal/app/network"
)

var errSubnetListCIDR = errors.New("the subnet list CIDR filter requires an address with a prefix length")
var errSubnetListDHCP = errors.New("the subnet list enable_dhcp filter requires a boolean")

func parseSubnetListQuery(rawQuery string) (app.SubnetListFilter, error) {
	query, err := url.ParseQuery(rawQuery)
	if err != nil {
		return app.SubnetListFilter{}, errors.New("the subnet list query is malformed")
	}
	for field := range query {
		switch field {
		case "name", "network_id", "cidr", "enable_dhcp":
		default:
			return app.SubnetListFilter{}, errors.New(
				"subnet lists accept only name, network_id, cidr, and enable_dhcp query parameters",
			)
		}
	}
	filter := app.SubnetListFilter{
		Names:      query["name"],
		NetworkIDs: query["network_id"],
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
