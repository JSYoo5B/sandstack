package network

import (
	"errors"
	"net/url"
	"strings"

	app "github.com/JSYoo5B/sandstack/internal/app/network"
)

var errPortListAdminState = errors.New("the port list admin_state_up filter requires a boolean")

func parsePortListQuery(rawQuery string) (app.PortListFilter, error) {
	query, err := url.ParseQuery(rawQuery)
	if err != nil {
		return app.PortListFilter{}, errors.New("The port list query is malformed.")
	}
	for key := range query {
		switch key {
		case "id", "name", "admin_state_up", "network_id", "device_id", "device_owner", "fixed_ips":
		default:
			return app.PortListFilter{}, errors.New(
				"Port lists accept id, name, admin_state_up, network_id, device_id, device_owner, and fixed_ips.",
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
