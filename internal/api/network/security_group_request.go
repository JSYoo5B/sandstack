package network

import (
	"encoding/json"
	"errors"
	"unicode/utf8"
)

var errInvalidSecurityGroupRequest = errors.New("expected one security_group object with valid fields")

type createSecurityGroupParams struct {
	name        string
	description string
	stateful    bool
	requestOwnership
}

func parseCreateSecurityGroup(data []byte) (createSecurityGroupParams, error) {
	fields, err := readCreateFields(data, "security_group")
	if err != nil {
		return createSecurityGroupParams{}, errInvalidSecurityGroupRequest
	}
	params := createSecurityGroupParams{stateful: true}
	for field, value := range fields {
		var target any
		switch field {
		case "name":
			target = &params.name
		case "description":
			target = &params.description
		case "stateful":
			var text string
			if json.Unmarshal(value, &text) != nil {
				text = string(value)
			}
			stateful, ok := parseBooleanFilters([]string{text})
			if !ok {
				return createSecurityGroupParams{}, errInvalidSecurityGroupRequest
			}
			params.stateful = stateful[0]
			continue
		case "project_id":
			target = &params.projectID
		case "tenant_id":
			target = &params.tenantID
		default:
			return createSecurityGroupParams{}, errInvalidSecurityGroupRequest
		}
		if !decodeCreateField(value, target) {
			return createSecurityGroupParams{}, errInvalidSecurityGroupRequest
		}
	}
	if utf8.RuneCountInString(params.name) > maxResourceNameLength ||
		utf8.RuneCountInString(params.description) > maxResourceNameLength {
		return createSecurityGroupParams{}, errors.New("security group name and description may contain up to 255 characters")
	}
	return params, nil
}
