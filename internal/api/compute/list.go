package compute

import (
	"net/http"
	"net/url"
	"strings"

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
	statuses := query["status"]
	if len(query) != 0 && (len(query) != 1 || len(statuses) != 1) {
		writeFault(w, http.StatusBadRequest, "badRequest", "Only one status query parameter is supported.")
		return
	}
	var status *domaincompute.Status
	if len(statuses) == 1 {
		requested := domaincompute.Status(strings.ToLower(statuses[0]))
		status = &requested
	}

	servers := h.service.List(scope.projectID, status)
	response := serversResponse{Servers: make([]serverBody, len(servers))}
	for i, server := range servers {
		response.Servers[i] = h.serverBody(r, server, scope.pathProject)
	}
	writeJSON(w, http.StatusOK, response)
}
