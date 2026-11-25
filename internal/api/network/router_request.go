package network

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/netip"
	"unicode/utf8"

	app "github.com/JSYoo5B/sandstack/internal/app/network"
	domain "github.com/JSYoo5B/sandstack/internal/domain/network"
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
		case "routes":
			if ownership != nil {
				return errInvalidRouterRequest
			}
			changes.Routes, err = parseRouterRoutes(value)
			if err != nil {
				return err
			}
			continue
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

var errInvalidRouterRouteRequest = errors.New("routes must be an array of IPv4 destination CIDRs and next-hop addresses")

func parseRouterRouteAction(data []byte) ([]domain.RouterRoute, error) {
	fields, err := readCreateFields(data, "router")
	if err != nil || len(fields) != 1 || bytes.Equal(fields["routes"], []byte("null")) {
		return nil, errInvalidRouterRouteRequest
	}
	return parseRouterRoutes(fields["routes"])
}

func parseRouterRoutes(data json.RawMessage) ([]domain.RouterRoute, error) {
	var entries []json.RawMessage
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, errInvalidRouterRouteRequest
	}
	// Neutron normalizes a null route list to an explicit empty replacement.
	routes := make([]domain.RouterRoute, 0, len(entries))
	for _, entry := range entries {
		fields, err := readUniqueObject(entry)
		var destination, nextHop string
		if err != nil || len(fields) != 2 || !decodeCreateField(fields["destination"], &destination) ||
			!decodeCreateField(fields["nexthop"], &nextHop) {
			return nil, errInvalidRouterRouteRequest
		}
		prefix, err := netip.ParsePrefix(destination)
		if err != nil || !prefix.Addr().Is4() || prefix != prefix.Masked() || prefix.Addr().IsLoopback() {
			return nil, errInvalidRouterRouteRequest
		}
		address, err := netip.ParseAddr(nextHop)
		if err != nil || !address.Is4() {
			return nil, errInvalidRouterRouteRequest
		}
		routes = append(routes, domain.RouterRoute{Destination: prefix, NextHop: address})
	}
	return routes, nil
}
