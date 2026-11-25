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

func (h handler) listServersDetailed(w http.ResponseWriter, r *http.Request, scope requestScope) {
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		writeFault(w, http.StatusBadRequest, "badRequest", "The server list query is malformed.")
		return
	}
	var filter appcompute.ListFilter
	for field, values := range query {
		if len(values) != 1 || (field != "name" && field != "status") {
			writeFault(w, http.StatusBadRequest, "badRequest", "Only one name and one status query parameter are supported.")
			return
		}
		value := values[0]
		switch field {
		case "status":
			requested := domaincompute.Status(strings.ToLower(value))
			filter.Status = &requested
		case "name":
			filter.Name, err = regexp.Compile(value)
			if value == "" || !utf8.ValidString(value) || err != nil {
				writeFault(w, http.StatusBadRequest, "badRequest", "The name filter must be a nonempty regular expression.")
				return
			}
		}
	}

	servers := h.service.List(scope.projectID, filter)
	response := serversResponse{Servers: make([]serverBody, len(servers))}
	for i, server := range servers {
		response.Servers[i] = h.serverBody(r, server, scope.pathProject)
	}
	writeJSON(w, http.StatusOK, response)
}
