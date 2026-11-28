package network

import (
	"encoding/json"
	"errors"
	"unicode/utf8"

	app "github.com/JSYoo5B/sandstack/internal/app/network"
)

var (
	errInvalidSecurityGroupRequest = errors.New("expected one security_group object with valid fields")
	errSecurityGroupTextLength     = errors.New("security group name and description may contain up to 255 characters")
)

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
			stateful, ok := parseSecurityGroupStateful(value)
			if !ok {
				return createSecurityGroupParams{}, errInvalidSecurityGroupRequest
			}
			params.stateful = stateful
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
	if !validSecurityGroupText(params.name, params.description) {
		return createSecurityGroupParams{}, errSecurityGroupTextLength
	}
	return params, nil
}

func parseUpdateSecurityGroup(data []byte) (app.SecurityGroupUpdate, error) {
	fields, err := readCreateFields(data, "security_group")
	if err != nil {
		return app.SecurityGroupUpdate{}, errInvalidSecurityGroupRequest
	}
	var changes app.SecurityGroupUpdate
	for field, value := range fields {
		var target any
		switch field {
		case "name":
			target = &changes.Name
		case "description":
			target = &changes.Description
		case "stateful":
			stateful, ok := parseSecurityGroupStateful(value)
			if !ok {
				return app.SecurityGroupUpdate{}, errInvalidSecurityGroupRequest
			}
			changes.Stateful = &stateful
			continue
		default:
			return app.SecurityGroupUpdate{}, errInvalidSecurityGroupRequest
		}
		if !decodeCreateField(value, target) {
			return app.SecurityGroupUpdate{}, errInvalidSecurityGroupRequest
		}
	}
	var name, description string
	if changes.Name != nil {
		name = *changes.Name
	}
	if changes.Description != nil {
		description = *changes.Description
	}
	if !validSecurityGroupText(name, description) {
		return app.SecurityGroupUpdate{}, errSecurityGroupTextLength
	}
	return changes, nil
}

func parseSecurityGroupStateful(value json.RawMessage) (bool, bool) {
	var text string
	if json.Unmarshal(value, &text) != nil {
		text = string(value)
	}
	stateful, ok := parseBooleanFilters([]string{text})
	if !ok {
		return false, false
	}
	return stateful[0], true
}

func validSecurityGroupText(name, description string) bool {
	return utf8.RuneCountInString(name) <= maxResourceNameLength &&
		utf8.RuneCountInString(description) <= maxResourceNameLength
}
