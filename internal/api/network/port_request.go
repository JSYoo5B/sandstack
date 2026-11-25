package network

import (
	"encoding/json"
	"errors"
	"strings"
)

var errInvalidPortRequest = errors.New("expected one port object with valid fields")
var errInvalidFixedIPs = errors.New("fixed_ips must be empty or contain one subnet UUID")

type createPortParams struct {
	networkID    string
	name         string
	adminStateUp bool
	subnetID     *string
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
	params.subnetID, err = parsePortSubnet(fields["fixed_ips"])
	if err != nil {
		return createPortParams{}, err
	}
	return params, nil
}

func parsePortSubnet(value json.RawMessage) (*string, error) {
	if value == nil {
		return nil, nil
	}
	var requested []json.RawMessage
	if !decodeCreateField(value, &requested) || len(requested) > 1 {
		return nil, errInvalidFixedIPs
	}
	var subnetID string
	if len(requested) == 1 {
		fields, err := readUniqueObject(requested[0])
		if err != nil || len(fields) != 1 || !decodeCreateField(fields["subnet_id"], &subnetID) ||
			!resourceUUIDPattern.MatchString(subnetID) {
			return nil, errInvalidFixedIPs
		}
		subnetID = strings.ToLower(subnetID)
	}
	return &subnetID, nil
}
