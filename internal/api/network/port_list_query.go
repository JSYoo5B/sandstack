package network

import (
	"errors"
	"net/url"
	"slices"
	"strings"

	app "github.com/JSYoo5B/sandstack/internal/app/network"
)

var errPortListAdminState = errors.New("the port list admin_state_up filter requires a boolean")

func parsePortListQuery(query url.Values) (app.PortListFilter, error) {
	for key := range query {
		switch key {
		case "id", "name", "admin_state_up", "network_id", "device_id", "device_owner", "fixed_ips",
			"limit", "marker", "page_reverse", "sort_key", "sort_dir", "tags", "tags-any", "not-tags", "not-tags-any":
		default:
			return app.PortListFilter{}, errors.New("The port list query contains an unsupported parameter.")
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
		Tags:         parseTagFilter(query),
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

func parsePortListOrder(query url.Values) (app.PortListOrder, error) {
	empty := func(value string) bool { return value == "" }
	keys := slices.DeleteFunc(slices.Clone(query["sort_key"]), empty)
	directions := slices.DeleteFunc(slices.Clone(query["sort_dir"]), empty)
	if len(keys) == 0 && len(directions) == 0 {
		return app.PortOrderID, nil
	}
	if len(keys) == 1 && keys[0] == "name" && len(directions) == 1 {
		switch directions[0] {
		case "asc":
			return app.PortOrderNameAscending, nil
		case "desc":
			return app.PortOrderNameDescending, nil
		}
	}
	return app.PortOrderID, errors.New("Port name sorting requires sort_key=name and sort_dir=asc or desc.")
}
