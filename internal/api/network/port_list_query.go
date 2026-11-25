package network

import (
	"errors"
	"net/url"
	"strconv"
	"strings"

	app "github.com/JSYoo5B/sandstack/internal/app/network"
)

var errPortListAdminState = errors.New("the port list admin_state_up filter requires a boolean")

func parsePortListQuery(query url.Values) (app.PortListFilter, error) {
	for key := range query {
		switch key {
		case "id", "name", "admin_state_up", "network_id", "device_id", "device_owner", "fixed_ips", "limit", "marker":
		default:
			return app.PortListFilter{}, errors.New(
				"Port lists accept id, name, admin_state_up, network_id, device_id, device_owner, fixed_ips, limit, and marker.",
			)
		}
	}
	states, ok := parseBooleanFilters(query["admin_state_up"])
	if !ok {
		return app.PortListFilter{}, errPortListAdminState
	}
	filter := app.PortListFilter{
		IDs:          query["id"],
		Names:        query["name"],
		AdminStates:  states,
		NetworkIDs:   query["network_id"],
		DeviceIDs:    query["device_id"],
		DeviceOwners: query["device_owner"],
	}
	for _, entry := range query["fixed_ips"] {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			switch strings.TrimSpace(key) {
			case "ip_address":
				filter.FixedAddresses = append(filter.FixedAddresses, strings.TrimSpace(value))
				continue
			case "subnet_id":
				filter.FixedSubnetIDs = append(filter.FixedSubnetIDs, strings.TrimSpace(value))
				continue
			}
		}
		return app.PortListFilter{}, errors.New("A fixed_ips filter requires ip_address=value or subnet_id=value.")
	}
	return filter, nil
}

func parsePortPageLimit(query url.Values) (int, error) {
	if len(query["limit"]) > 1 || len(query["marker"]) > 1 {
		return 0, errors.New("Port pagination parameters cannot be repeated.")
	}
	values := query["limit"]
	if len(values) == 0 {
		return 0, nil
	}
	limit, err := strconv.Atoi(strings.TrimSpace(values[0]))
	if err != nil || limit < 0 {
		return 0, errors.New("The port list limit must be an integer of zero or more.")
	}
	return limit, nil
}
