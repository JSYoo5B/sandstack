package network

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	app "github.com/JSYoo5B/sandstack/internal/app/network"
)

func parsePageOptions(query url.Values) (app.PageOptions, error) {
	if len(query["limit"]) > 1 || len(query["marker"]) > 1 || len(query["page_reverse"]) > 1 {
		return app.PageOptions{}, errors.New("Pagination parameters cannot be repeated.")
	}
	values := query["limit"]
	var limit int
	if len(values) != 0 {
		var err error
		limit, err = strconv.Atoi(strings.TrimSpace(values[0]))
		if err != nil || limit < 0 {
			return app.PageOptions{}, errors.New("The list limit must be an integer of zero or more.")
		}
	}
	return app.PageOptions{
		Marker:  query.Get("marker"),
		Limit:   limit,
		Reverse: strings.EqualFold(query.Get("page_reverse"), "true"),
	}, nil
}

type pageLink struct {
	Rel  string `json:"rel"`
	Href string `json:"href"`
}

func (h handler) pageLinks(r *http.Request, path string, query url.Values, next, previous string) []pageLink {
	if next == "" && previous == "" {
		return nil
	}
	var links []pageLink
	endpoint := h.endpointURL(r, path)
	if next != "" {
		query.Set("marker", next)
		query.Del("page_reverse")
		links = append(links, pageLink{Rel: "next", Href: endpoint + "?" + query.Encode()})
	}
	if previous != "" {
		query.Set("marker", previous)
		query.Set("page_reverse", "True")
		links = append(links, pageLink{Rel: "previous", Href: endpoint + "?" + query.Encode()})
	}
	return links
}
