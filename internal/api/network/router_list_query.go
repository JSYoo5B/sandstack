package network

import (
	"errors"
	"net/url"

	app "github.com/JSYoo5B/sandstack/internal/app/network"
)

var errRouterListAdminState = errors.New("the router list admin_state_up filter requires a boolean")

func parseRouterListQuery(query url.Values) (app.RouterListFilter, error) {
	for field := range query {
		switch field {
		case "name", "admin_state_up", "limit", "marker", "page_reverse", "tags", "tags-any", "not-tags", "not-tags-any":
		default:
			return app.RouterListFilter{}, errors.New("the router list query contains an unsupported parameter")
		}
	}
	states, ok := parseBooleanFilters(query["admin_state_up"])
	if !ok {
		return app.RouterListFilter{}, errRouterListAdminState
	}
	return app.RouterListFilter{Names: query["name"], AdminStates: states, Tags: parseTagFilter(query)}, nil
}
