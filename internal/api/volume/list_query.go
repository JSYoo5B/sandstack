package volume

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	app "github.com/JSYoo5B/sandstack/internal/app/volume"
	domain "github.com/JSYoo5B/sandstack/internal/domain/volume"
)

const maxDetailPageLimit = 1000

type detailListOptions struct {
	filter app.ListFilter
	marker *string
	limit  int
}

func parseDetailListQuery(query url.Values, resource string) (detailListOptions, error) {
	options := detailListOptions{limit: maxDetailPageLimit}
	for field, values := range query {
		if len(values) != 1 {
			return detailListOptions{}, errors.New(resource + " list parameters accept one value each")
		}
		switch field {
		case "name":
			options.filter.Name = &values[0]
		case "status":
			status := domain.State(values[0])
			options.filter.Status = &status
		case "marker":
			options.marker = &values[0]
		case "limit":
			limit, err := strconv.Atoi(strings.TrimSpace(values[0]))
			if err != nil || limit < 0 {
				return detailListOptions{}, errors.New("the " + resource + " list limit must be a nonnegative integer")
			}
			options.limit = min(limit, maxDetailPageLimit)
		case "sort":
			if values[0] != "id:asc" {
				return detailListOptions{}, errors.New("the " + resource + " list supports only sort=id:asc")
			}
		default:
			return detailListOptions{}, errors.New("the " + resource + " list query contains an unsupported parameter")
		}
	}
	return options, nil
}

func (h handler) detailListLinks(r *http.Request, query url.Values, path, nextMarker string) []volumeLink {
	if nextMarker == "" {
		return nil
	}
	query.Set("marker", nextMarker)
	return []volumeLink{{Rel: "next", Href: h.endpointURL(r, path) + "?" + query.Encode()}}
}
