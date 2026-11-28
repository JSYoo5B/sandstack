package network

import (
	"net/url"
	"slices"
	"strings"

	app "github.com/JSYoo5B/sandstack/internal/app/network"
)

// parseBooleanFilters accepts the same values as Neutron's boolean converter.
func parseBooleanFilters(values []string) ([]bool, bool) {
	var states []bool
	for _, value := range values {
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "1", "t", "true", "on", "y", "yes":
			states = append(states, true)
		case "0", "f", "false", "off", "n", "no":
			states = append(states, false)
		default:
			return nil, false
		}
	}
	return states, true
}

func parseTagFilter(query url.Values) app.TagFilter {
	split := func(key string) []string {
		var tags []string
		for _, value := range query[key] {
			for _, tag := range strings.Split(value, ",") {
				if !slices.Contains(tags, tag) {
					tags = append(tags, tag)
				}
			}
		}
		return tags
	}
	return app.TagFilter{
		All:    split("tags"),
		Any:    split("tags-any"),
		NotAll: split("not-tags"),
		NotAny: split("not-tags-any"),
	}
}
