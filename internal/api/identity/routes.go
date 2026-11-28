package identity

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	appidentity "github.com/JSYoo5B/sandstack/internal/app/identity"
)

const identityPath = "/identity/v3"

type handler struct {
	service     *appidentity.Service
	endpointURL func(*http.Request, string) string
	entries     []CatalogEntry
}

// Register installs the supported Identity v3 endpoints on the root router.
func Register(
	router chi.Router,
	service *appidentity.Service,
	endpointURL func(*http.Request, string) string,
	additionalEntries ...CatalogEntry,
) {
	entries := append([]CatalogEntry{{
		ServiceID:  "f67f532d68cc4616bc5b81a6eb84ab7f",
		EndpointID: "9a077ab4b7f0463b95f1baccde36b964",
		Type:       "identity",
		Name:       "keystone",
		Path:       identityPath,
	}}, additionalEntries...)
	h := handler{service: service, endpointURL: endpointURL, entries: entries}
	authPath := identityPath + "/auth"
	router.Post(authPath+"/tokens", h.issueToken)
	router.Get(authPath+"/catalog", h.getCatalog)
	router.Head(authPath+"/catalog", h.getCatalog)
	credentialsPath := identityPath + "/users/{userID}/application_credentials"
	router.Post(credentialsPath, h.createApplicationCredential)
	router.Get(credentialsPath, h.listApplicationCredentials)
	router.Head(credentialsPath, h.listApplicationCredentials)
	router.Get(credentialsPath+"/{credentialID}", h.getApplicationCredential)
	router.Head(credentialsPath+"/{credentialID}", h.getApplicationCredential)
	router.Delete(credentialsPath+"/{credentialID}", h.deleteApplicationCredential)
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
