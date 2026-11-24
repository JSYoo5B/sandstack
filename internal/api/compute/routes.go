package compute

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	appcompute "github.com/JSYoo5B/sandstack/internal/app/compute"
)

// Path is the base path of the Compute v2.1 service.
const Path = "/compute/v2.1"

type handler struct {
	service         *appcompute.Service
	projectForToken func(string) (string, error)
	endpointURL     func(*http.Request, string) string
}

// Register installs the supported Compute operations on the root router.
func Register(
	router chi.Router,
	service *appcompute.Service,
	projectForToken func(string) (string, error),
	endpointURL func(*http.Request, string) string,
) {
	h := handler{
		service: service, projectForToken: projectForToken, endpointURL: endpointURL,
	}
	router.Get(Path+"/servers/{serverID}", h.showServer)
	router.Get(Path+"/{projectID}/servers/{serverID}", h.showServer)
}

func (h handler) showServer(w http.ResponseWriter, r *http.Request) {
	tokens := r.Header.Values("X-Auth-Token")
	if len(tokens) != 1 || tokens[0] == "" {
		writeFault(w, http.StatusUnauthorized, "unauthorized", "A valid authentication token is required.")
		return
	}
	projectID, err := h.projectForToken(tokens[0])
	if err != nil || projectID == "" {
		writeFault(w, http.StatusUnauthorized, "unauthorized", "A valid authentication token is required.")
		return
	}
	if !acceptVersion(w, r) {
		return
	}

	pathProject := chi.URLParam(r, "projectID")
	if pathProject != "" && pathProject != projectID {
		writeFault(w, http.StatusBadRequest, "badRequest", "The URL project does not match the token's project.")
		return
	}
	server, err := h.service.Get(projectID, chi.URLParam(r, "serverID"))
	if errors.Is(err, appcompute.ErrServerNotFound) {
		writeFault(w, http.StatusNotFound, "itemNotFound", "The server could not be found.")
		return
	}
	if err != nil {
		writeFault(w, http.StatusInternalServerError, "computeFault", "Unable to read the server.")
		return
	}

	writeJSON(w, http.StatusOK, serverResponse{Server: h.serverBody(r, server, pathProject)})
}

type httpFault struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func writeFault(w http.ResponseWriter, code int, kind string, message string) {
	writeJSON(w, code, map[string]httpFault{kind: {Code: code, Message: message}})
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}
