package image

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	app "github.com/JSYoo5B/sandstack/internal/app/image"
)

const (
	// CatalogPath is the base URL to which Gophercloud adds its API version.
	CatalogPath = "/image"
	// Path is the base path of the Image v2 routes.
	Path = CatalogPath + "/v2"
)

type handler struct {
	service         *app.Service
	projectForToken func(string) (string, error)
	endpointURL     func(*http.Request, string) string
}

// Register installs the supported Image operations on the root router.
func Register(
	router chi.Router,
	service *app.Service,
	projectForToken func(string) (string, error),
	endpointURL func(*http.Request, string) string,
) {
	h := handler{service: service, projectForToken: projectForToken, endpointURL: endpointURL}
	router.Get(CatalogPath, h.versions)
	router.Get(CatalogPath+"/", h.versions)
	router.Get(Path+"/images/{imageID}", h.showImage)
}

type versionInfo struct {
	ID     string        `json:"id"`
	Status string        `json:"status"`
	Links  []versionLink `json:"links"`
}

type versionLink struct {
	Rel  string `json:"rel"`
	Href string `json:"href"`
}

func (h handler) versions(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusMultipleChoices, struct {
		Versions []versionInfo `json:"versions"`
	}{Versions: []versionInfo{{
		ID:     "v2.0",
		Status: "CURRENT",
		Links:  []versionLink{{Rel: "self", Href: h.endpointURL(r, Path+"/")}},
	}}})
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}
