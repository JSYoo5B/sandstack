package network

import (
	"encoding/json"
	"errors"
	"net/netip"

	domain "github.com/JSYoo5B/sandstack/internal/domain/network"
)

const maxSubnetHostRoutes = 20

var errInvalidHostRoutes = errors.New("expected unique host routes with a network CIDR and a next-hop IP address")
var errHostRouteIPVersion = errors.New("host routes on an IPv4 subnet must use IPv4 destinations and next hops")
var errHostRouteLimit = errors.New("the number of subnet host routes exceeds the limit 20")

func parseSubnetHostRoutes(data json.RawMessage) ([]domain.SubnetHostRoute, error) {
	var entries []json.RawMessage
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, errInvalidHostRoutes
	}
	routes := make([]domain.SubnetHostRoute, 0, len(entries))
	seen := make(map[domain.SubnetHostRoute]struct{}, len(entries))
	for _, entry := range entries {
		fields, err := readUniqueObject(entry)
		var destination, nextHop string
		if err != nil || len(fields) != 2 || !decodeCreateField(fields["destination"], &destination) ||
			!decodeCreateField(fields["nexthop"], &nextHop) {
			return nil, errInvalidHostRoutes
		}
		prefix, err := netip.ParsePrefix(destination)
		if err != nil || prefix != prefix.Masked() || prefix.Addr().IsLoopback() {
			return nil, errInvalidHostRoutes
		}
		address, err := netip.ParseAddr(nextHop)
		if err != nil || address.Zone() != "" {
			return nil, errInvalidHostRoutes
		}
		route := domain.SubnetHostRoute{Destination: prefix, NextHop: address}
		if _, duplicate := seen[route]; duplicate {
			return nil, errInvalidHostRoutes
		}
		seen[route] = struct{}{}
		routes = append(routes, route)
	}
	if len(routes) > maxSubnetHostRoutes {
		return nil, errHostRouteLimit
	}
	for _, route := range routes {
		if !route.Destination.Addr().Is4() || !route.NextHop.Is4() {
			return nil, errHostRouteIPVersion
		}
	}
	return routes, nil
}
