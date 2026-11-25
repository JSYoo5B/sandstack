package network

import (
	"errors"
	"unicode/utf8"

	app "github.com/JSYoo5B/sandstack/internal/app/network"
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
	params := createRouterParams{adminStateUp: true}
	fields := app.RouterUpdate{
		Name: &params.name, Description: &params.description, AdminStateUp: &params.adminStateUp,
	}
	err := parseRouterFields(data, &fields, &params.requestOwnership)
	return params, err
}

func parseRouterFields(data []byte, changes *app.RouterUpdate, ownership *requestOwnership) error {
	fields, err := readCreateFields(data, "router")
	if err != nil {
		return errInvalidRouterRequest
	}
	for field, value := range fields {
		var target any
		switch field {
		case "name":
			target = &changes.Name
		case "description":
			target = &changes.Description
		case "admin_state_up":
			target = &changes.AdminStateUp
		case "project_id":
			if ownership == nil {
				return errInvalidRouterRequest
			}
			target = &ownership.projectID
		case "tenant_id":
			if ownership == nil {
				return errInvalidRouterRequest
			}
			target = &ownership.tenantID
		default:
			return errInvalidRouterRequest
		}
		if !decodeCreateField(value, target) {
			return errInvalidRouterRequest
		}
	}
	if changes.Name != nil && !validResourceName(*changes.Name) {
		return errors.New("router name must be printable and contain at most 255 characters")
	}
	if changes.Description != nil && utf8.RuneCountInString(*changes.Description) > maxRouterDescriptionLength {
		return errors.New("router description may contain up to 255 characters")
	}
	return nil
}
