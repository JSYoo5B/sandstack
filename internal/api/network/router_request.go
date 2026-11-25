package network

import (
	"errors"
	"unicode/utf8"
)

const maxRouterDescriptionLength = 255

var errInvalidRouterRequest = errors.New("expected one router object with valid fields")

type createRouterParams struct {
	name         string
	description  string
	adminStateUp bool
	requestOwnership
}

func parseCreateRouter(data []byte) (createRouterParams, error) {
	fields, err := readCreateFields(data, "router")
	if err != nil {
		return createRouterParams{}, errInvalidRouterRequest
	}
	params := createRouterParams{adminStateUp: true}
	for field, value := range fields {
		var target any
		switch field {
		case "name":
			target = &params.name
		case "description":
			target = &params.description
		case "admin_state_up":
			target = &params.adminStateUp
		case "project_id":
			target = &params.projectID
		case "tenant_id":
			target = &params.tenantID
		default:
			return createRouterParams{}, errInvalidRouterRequest
		}
		if !decodeCreateField(value, target) {
			return createRouterParams{}, errInvalidRouterRequest
		}
	}
	if !validResourceName(params.name) {
		return createRouterParams{}, errors.New("router name must be printable and contain at most 255 characters")
	}
	if utf8.RuneCountInString(params.description) > maxRouterDescriptionLength {
		return createRouterParams{}, errors.New("router description may contain up to 255 characters")
	}
	return params, nil
}
