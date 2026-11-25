package network

import (
	"encoding/json"
	"errors"
	"net/netip"
	"strings"

	app "github.com/JSYoo5B/sandstack/internal/app/network"
)

var errInvalidPortRequest = errors.New("expected one port object with valid fields")
var errInvalidFixedIPs = errors.New("fixed_ips must be empty or contain one subnet UUID, IPv4 address, or both")

type createPortParams struct {
	networkID    string
	name         string
	adminStateUp bool
	subnetID     *string
	fixedIP      netip.Addr
	requestOwnership
}

func parseCreatePort(data []byte) (createPortParams, error) {
	fields, err := readCreateFields(data, "port")
	if err != nil {
		return createPortParams{}, errInvalidPortRequest
	}
	params := createPortParams{adminStateUp: true}
	for field, value := range fields {
		var target any
		switch field {
		case "network_id":
			target = &params.networkID
		case "name":
			target = &params.name
		case "admin_state_up":
			target = &params.adminStateUp
		case "fixed_ips":
			continue
		case "project_id":
			target = &params.projectID
		case "tenant_id":
			target = &params.tenantID
		default:
			return createPortParams{}, errInvalidPortRequest
		}
		if !decodeCreateField(value, target) {
			return createPortParams{}, errInvalidPortRequest
		}
	}
	if !resourceUUIDPattern.MatchString(params.networkID) {
		return createPortParams{}, errors.New("expected a network UUID")
	}
	if !validResourceName(params.name) {
		return createPortParams{}, errors.New("port name must be printable and contain at most 255 characters")
	}
	params.networkID = strings.ToLower(params.networkID)
	params.subnetID, params.fixedIP, err = parsePortFixedIP(fields["fixed_ips"])
	if err != nil {
		return createPortParams{}, err
	}
	return params, nil
}

func parseUpdatePort(data []byte) (app.PortUpdate, error) {
	fields, err := readCreateFields(data, "port")
	if err != nil {
		return app.PortUpdate{}, errInvalidPortRequest
	}
	var changes app.PortUpdate
	for field, value := range fields {
		var target any
		switch field {
		case "name":
			target = &changes.Name
		case "admin_state_up":
			target = &changes.AdminStateUp
		case "fixed_ips":
			subnetID, address, err := parsePortFixedIP(value)
			if err != nil {
				return app.PortUpdate{}, err
			}
			if subnetID != nil && *subnetID == "" {
				subnetID = nil
			}
			changes.FixedIPs = &app.PortFixedIPUpdate{SubnetID: subnetID, Address: address}
			continue
		default:
			return app.PortUpdate{}, errInvalidPortRequest
		}
		if !decodeCreateField(value, target) {
			return app.PortUpdate{}, errInvalidPortRequest
		}
	}
	if changes.Name != nil && !validResourceName(*changes.Name) {
		return app.PortUpdate{}, errors.New("port name must be printable and contain at most 255 characters")
	}
	return changes, nil
}

func parsePortFixedIP(value json.RawMessage) (*string, netip.Addr, error) {
	if value == nil {
		return nil, netip.Addr{}, nil
	}
	var requested []json.RawMessage
	if !decodeCreateField(value, &requested) || len(requested) > 1 {
		return nil, netip.Addr{}, errInvalidFixedIPs
	}
	if len(requested) == 0 {
		noSubnet := ""
		return &noSubnet, netip.Addr{}, nil
	}
	fields, err := readUniqueObject(requested[0])
	if err != nil || len(fields) == 0 {
		return nil, netip.Addr{}, errInvalidFixedIPs
	}
	var subnetID *string
	var fixedIP netip.Addr
	for field, value := range fields {
		var text string
		if !decodeCreateField(value, &text) {
			return nil, netip.Addr{}, errInvalidFixedIPs
		}
		switch field {
		case "subnet_id":
			if !resourceUUIDPattern.MatchString(text) {
				return nil, netip.Addr{}, errInvalidFixedIPs
			}
			id := strings.ToLower(text)
			subnetID = &id
		case "ip_address":
			fixedIP, err = netip.ParseAddr(text)
			if err != nil || !fixedIP.Is4() {
				return nil, netip.Addr{}, errInvalidFixedIPs
			}
		default:
			return nil, netip.Addr{}, errInvalidFixedIPs
		}
	}
	return subnetID, fixedIP, nil
}
