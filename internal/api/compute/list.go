package compute

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"

	appcompute "github.com/JSYoo5B/sandstack/internal/app/compute"
	domaincompute "github.com/JSYoo5B/sandstack/internal/domain/compute"
)

type serversResponse struct {
	Servers []serverBody `json:"servers"`
}

type serverSummaryBody struct {
	ID    string         `json:"id"`
	Name  string         `json:"name"`
	Links []resourceLink `json:"links"`
}

func (h handler) listServersBasic(w http.ResponseWriter, r *http.Request, scope requestScope) {
	filter, fault := parseServerListFilter(r.URL.RawQuery)
	if fault != "" {
		writeFault(w, http.StatusBadRequest, "badRequest", fault)
		return
	}
	servers := h.service.ListBasic(scope.projectID, filter)
	summaries := make([]serverSummaryBody, len(servers))
	projectPath := computeProjectPath(scope.pathProject)
	for i, server := range servers {
		summaries[i] = serverSummaryBody{
			ID:    server.ID,
			Name:  server.Name,
			Links: h.serverLinks(r, server.ID, projectPath),
		}
	}
	writeJSON(w, http.StatusOK, struct {
		Servers []serverSummaryBody `json:"servers"`
	}{Servers: summaries})
}

func (h handler) listServersDetailed(w http.ResponseWriter, r *http.Request, scope requestScope) {
	filter, fault := parseServerListFilter(r.URL.RawQuery)
	if fault != "" {
		writeFault(w, http.StatusBadRequest, "badRequest", fault)
		return
	}
	servers := h.service.List(scope.projectID, filter)
	response := serversResponse{Servers: make([]serverBody, len(servers))}
	for i, server := range servers {
		response.Servers[i] = h.serverBody(r, server, scope.pathProject)
	}
	writeJSON(w, http.StatusOK, response)
}

func parseServerListFilter(rawQuery string) (appcompute.ListFilter, string) {
	query, err := url.ParseQuery(rawQuery)
	if err != nil {
		return appcompute.ListFilter{}, "The server list query is malformed."
	}
	var filter appcompute.ListFilter
	for field, values := range query {
		if len(values) != 1 || (field != "name" && field != "status") {
			return appcompute.ListFilter{}, "Only one name and one status query parameter are supported."
		}
		value := values[0]
		switch field {
		case "status":
			requested := domaincompute.Status(strings.ToLower(value))
			filter.Status = &requested
		case "name":
			filter.Name, err = regexp.Compile(value)
			if value == "" || !utf8.ValidString(value) || err != nil {
				return appcompute.ListFilter{}, "The name filter must be a nonempty regular expression."
			}
		}
	}

	return filter, ""
}
