package network

import (
	"errors"
	"net/netip"
	"net/url"
	"strings"

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
	for _, value := range query["enable_dhcp"] {
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "1", "t", "true", "on", "y", "yes":
			filter.DHCPStates = append(filter.DHCPStates, true)
		case "0", "f", "false", "off", "n", "no":
			filter.DHCPStates = append(filter.DHCPStates, false)
		default:
			return app.SubnetListFilter{}, errSubnetListDHCP
		}
	}
	return filter, nil
}
