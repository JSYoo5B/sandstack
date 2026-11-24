package identity

import (
	"encoding/json"
	"net/http"
	"net/url"

	"github.com/go-chi/chi/v5"

	appidentity "github.com/JSYoo5B/sandstack/internal/app/identity"
)

const identityPath = "/identity/v3"

type handler struct {
	service   *appidentity.Service
	publicURL *url.URL
}

// Register installs the supported Identity v3 endpoints on the root router.
// publicURL is the configured external origin, or nil to use the request origin.
func Register(router chi.Router, service *appidentity.Service, publicURL *url.URL) {
	h := handler{service: service, publicURL: publicURL}
	authPath := identityPath + "/auth"
	router.Post(authPath+"/tokens", h.issueToken)
	router.Get(authPath+"/catalog", h.getCatalog)
	router.Head(authPath+"/catalog", h.getCatalog)
}

func (h handler) endpointURL(r *http.Request, path string) string {
	if h.publicURL != nil {
		return h.publicURL.ResolveReference(&url.URL{Path: path}).String()
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return (&url.URL{Scheme: scheme, Host: r.Host, Path: path}).String()
}

type errorResponse struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    int    `json:"code"`
	Title   string `json:"title"`
	Message string `json:"message"`
}

func (h handler) writeError(w http.ResponseWriter, r *http.Request, status int, message string) {
	if status == http.StatusUnauthorized {
		w.Header().Set("WWW-Authenticate", "Keystone uri=\""+h.endpointURL(r, identityPath)+"\"")
	}
	writeJSON(w, r, status, errorResponse{Error: errorDetail{
		Code: status, Title: http.StatusText(status), Message: message,
	}})
}

func writeJSON(w http.ResponseWriter, r *http.Request, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		// The response has already started, so a write failure cannot be retried.
		_ = json.NewEncoder(w).Encode(body)
	}
}
