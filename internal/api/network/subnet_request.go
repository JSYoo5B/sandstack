package network

import (
	"errors"
	"strings"
)

var errInvalidSubnetRequest = errors.New("expected one subnet object with valid fields")

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
		return createSubnetParams{}, errors.New("subnet name must be printable and contain at most 255 characters")
	}
	params.networkID = strings.ToLower(params.networkID)
	return params, nil
}
