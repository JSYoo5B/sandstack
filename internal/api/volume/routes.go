package volume

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	app "github.com/JSYoo5B/sandstack/internal/app/volume"
)

const (
	serviceRoot = "/volume"
	// Path is the versioned base path of the Block Storage service.
	Path = serviceRoot + "/v3"
)

type handler struct {
	service         *app.Service
	projectForToken func(string) (string, error)
	endpointURL     func(*http.Request, string) string
}

// Register installs the supported project-qualified Block Storage operations.
func Register(
	router chi.Router,
	service *app.Service,
	projectForToken func(string) (string, error),
	endpointURL func(*http.Request, string) string,
) {
	h := handler{service: service, projectForToken: projectForToken, endpointURL: endpointURL}
	router.Get(Path+"/{projectID}/volumes/detail", h.withProject(h.listVolumes))
	router.Get(Path+"/{projectID}/volumes/{volumeID}", h.withProject(h.showVolume))
}

func (h handler) withProject(next func(http.ResponseWriter, *http.Request, string)) http.HandlerFunc {
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
		if !acceptVersion(w, r) {
			return
		}
		if chi.URLParam(r, "projectID") != projectID {
			writeFault(w, http.StatusBadRequest, "badRequest", "The URL project does not match the token's project.")
			return
		}
		if r.URL.RawQuery != "" {
			writeFault(w, http.StatusBadRequest, "badRequest", "Volume reads require an empty query string.")
			return
		}
		next(w, r, projectID)
	}
}

type httpFault struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", "Keystone")
	writeFault(w, http.StatusUnauthorized, "unauthorized", "A valid authentication token is required.")
}

func writeFault(w http.ResponseWriter, code int, kind, message string) {
	writeJSON(w, code, map[string]httpFault{kind: {Code: code, Message: message}})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
