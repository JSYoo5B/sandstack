package network

import (
	"errors"
	"strings"

	app "github.com/JSYoo5B/sandstack/internal/app/network"
)

var errInvalidSubnetRequest = errors.New("expected one subnet object with valid fields")
var errInvalidSubnetName = errors.New("subnet name must be printable and contain at most 255 characters")

type createSubnetParams struct {
	networkID  string
	name       string
	cidr       string
	ipVersion  int
	enableDHCP bool
	requestOwnership
}

func parseCreateSubnet(data []byte) (createSubnetParams, error) {
	fields, err := readCreateFields(data, "subnet")
	if err != nil {
		return createSubnetParams{}, errInvalidSubnetRequest
	}
	params := createSubnetParams{enableDHCP: true}
	for field, value := range fields {
		var target any
		switch field {
		case "network_id":
			target = &params.networkID
		case "name":
			target = &params.name
		case "cidr":
			target = &params.cidr
		case "ip_version":
			target = &params.ipVersion
		case "enable_dhcp":
			target = &params.enableDHCP
		case "project_id":
			target = &params.projectID
		case "tenant_id":
			target = &params.tenantID
		default:
			return createSubnetParams{}, errInvalidSubnetRequest
		}
		if !decodeCreateField(value, target) {
			return createSubnetParams{}, errInvalidSubnetRequest
		}
	}
	if !resourceUUIDPattern.MatchString(params.networkID) || params.cidr == "" || fields["ip_version"] == nil {
		return createSubnetParams{}, errors.New("expected a network UUID, CIDR, and IP version")
	}
	if !validResourceName(params.name) {
		return createSubnetParams{}, errInvalidSubnetName
	}
	params.networkID = strings.ToLower(params.networkID)
	return params, nil
}

func parseUpdateSubnet(data []byte) (app.SubnetUpdate, error) {
	fields, err := readCreateFields(data, "subnet")
	if err != nil {
		return app.SubnetUpdate{}, errInvalidSubnetRequest
	}
	var changes app.SubnetUpdate
	for field, value := range fields {
		var target any
		switch field {
		case "name":
			target = &changes.Name
		case "enable_dhcp":
			target = &changes.EnableDHCP
		default:
			return app.SubnetUpdate{}, errInvalidSubnetRequest
		}
		if !decodeCreateField(value, target) {
			return app.SubnetUpdate{}, errInvalidSubnetRequest
		}
	}
	if changes.Name != nil && !validResourceName(*changes.Name) {
		return app.SubnetUpdate{}, errInvalidSubnetName
	}
	return changes, nil
}
