package network

import (
	"errors"
	"net/url"

	app "github.com/JSYoo5B/sandstack/internal/app/network"
)

var errRouterListAdminState = errors.New("the router list admin_state_up filter requires a boolean")

func parseRouterListQuery(rawQuery string) (app.RouterListFilter, error) {
	query, err := url.ParseQuery(rawQuery)
	if err != nil {
		return app.RouterListFilter{}, errors.New("the router list query is malformed")
	}
	for field := range query {
		if field != "name" && field != "admin_state_up" {
			return app.RouterListFilter{}, errors.New(
				"router lists accept only name and admin_state_up query parameters",
			)
		}
	}
	states, ok := parseBooleanFilters(query["admin_state_up"])
	if !ok {
		return app.RouterListFilter{}, errRouterListAdminState
	}
	return app.RouterListFilter{Names: query["name"], AdminStates: states}, nil
}
