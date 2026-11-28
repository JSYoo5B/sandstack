package network

import app "github.com/JSYoo5B/sandstack/internal/app/network"

func parseUpdateNetwork(data []byte) (app.NetworkUpdate, error) {
	fields, err := readCreateFields(data, "network")
	if err != nil {
		return app.NetworkUpdate{}, errInvalidCreateRequest
	}
	var changes app.NetworkUpdate
	for field, value := range fields {
		var target any
		switch field {
		case "name":
			target = &changes.Name
		case "admin_state_up":
			target = &changes.AdminStateUp
		case "port_security_enabled":
			target = &changes.PortSecurityEnabled
		default:
			return app.NetworkUpdate{}, errInvalidCreateRequest
		}
		if !decodeCreateField(value, target) {
			return app.NetworkUpdate{}, errInvalidCreateRequest
		}
	}
	if changes.Name != nil && !validResourceName(*changes.Name) {
		return app.NetworkUpdate{}, errInvalidNetworkName
	}
	return changes, nil
}
