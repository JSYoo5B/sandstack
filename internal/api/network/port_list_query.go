package network

import (
	"errors"
	"net/url"
	"slices"
	"strconv"
	"strings"

	app "github.com/JSYoo5B/sandstack/internal/app/network"
)

var errPortListAdminState = errors.New("the port list admin_state_up filter requires a boolean")

func parsePortListQuery(query url.Values) (app.PortListFilter, error) {
	for key := range query {
		switch key {
		case "id", "name", "admin_state_up", "network_id", "device_id", "device_owner", "fixed_ips",
			"limit", "marker", "page_reverse", "sort_key", "sort_dir":
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

func parsePortPageOptions(query url.Values) (app.PortPageOptions, error) {
	if len(query["limit"]) > 1 || len(query["marker"]) > 1 || len(query["page_reverse"]) > 1 {
		return app.PortPageOptions{}, errors.New("Port pagination parameters cannot be repeated.")
	}
	values := query["limit"]
	var limit int
	if len(values) != 0 {
		var err error
		limit, err = strconv.Atoi(strings.TrimSpace(values[0]))
		if err != nil || limit < 0 {
			return app.PortPageOptions{}, errors.New("The port list limit must be an integer of zero or more.")
		}
	}
	order, err := parsePortListOrder(query)
	if err != nil {
		return app.PortPageOptions{}, err
	}
	return app.PortPageOptions{
		Marker:  query.Get("marker"),
		Limit:   limit,
		Order:   order,
		Reverse: strings.EqualFold(query.Get("page_reverse"), "true"),
	}, nil
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
