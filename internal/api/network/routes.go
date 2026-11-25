package network

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	app "github.com/JSYoo5B/sandstack/internal/app/network"
)

const (
	// CatalogPath is the base URL to which Gophercloud adds its API version.
	CatalogPath = "/network"
	// Path is the base path of the Networking v2.0 routes.
	Path = CatalogPath + "/v2.0"
)

type handler struct {
	service         *app.Service
	projectForToken func(string) (string, error)
	endpointURL     func(*http.Request, string) string
}

type projectHandler func(http.ResponseWriter, *http.Request, string)

// Register installs the supported Networking operations on the root router.
func Register(
	router chi.Router,
	service *app.Service,
	projectForToken func(string) (string, error),
	endpointURL func(*http.Request, string) string,
) {
	h := handler{service: service, projectForToken: projectForToken, endpointURL: endpointURL}
	router.Get(CatalogPath, h.listVersions)
	router.Get(CatalogPath+"/", h.listVersions)
	router.Get(Path+"/networks", h.withProject(h.listNetworks))
	router.Post(Path+"/networks", h.withProject(withEmptyQuery(h.createNetwork)))
	router.Get(Path+"/networks/{networkID}", h.withProject(withEmptyQuery(h.showNetwork)))
	router.Delete(Path+"/networks/{networkID}", h.withProject(withEmptyQuery(h.deleteNetwork)))
}

func (h handler) withProject(next projectHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tokens := r.Header.Values("X-Auth-Token")
		if len(tokens) != 1 || tokens[0] == "" {
			writeUnauthorized(w)
			return
		}
		projectID, err := h.projectForToken(tokens[0])
		if err != nil || projectID == "" {
			writeUnauthorized(w)
			return
		}
		next(w, r, projectID)
	}
}

func withEmptyQuery(next projectHandler) projectHandler {
	return func(w http.ResponseWriter, r *http.Request, projectID string) {
		if r.URL.RawQuery != "" {
			writeError(w, http.StatusBadRequest, "BadRequest", "Network operations require an empty query string.")
			return
		}
		next(w, r, projectID)
	}
}

type neutronError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
	Detail  string `json:"detail"`
}

type authError struct {
	Code    int    `json:"code"`
	Title   string `json:"title"`
	Message string `json:"message"`
}

func writeError(w http.ResponseWriter, status int, kind, message string) {
	writeJSON(w, status, struct {
		Error neutronError `json:"NeutronError"`
	}{Error: neutronError{Type: kind, Message: message}})
}

func writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", "Keystone")
	writeJSON(w, http.StatusUnauthorized, struct {
		Error authError `json:"error"`
	}{Error: authError{
		Code:    http.StatusUnauthorized,
		Title:   http.StatusText(http.StatusUnauthorized),
		Message: "A valid authentication token is required.",
	}})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
