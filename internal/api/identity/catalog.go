package identity

import (
	"errors"
	"net/http"

	appidentity "github.com/JSYoo5B/sandstack/internal/app/identity"
)

// CatalogEntry describes a public service endpoint registered in this server.
type CatalogEntry struct {
	ServiceID     string
	EndpointID    string
	Type          string
	Name          string
	Path          string
	ProjectScoped bool
}

type catalogService struct {
	ID        string            `json:"id"`
	Type      string            `json:"type"`
	Name      string            `json:"name"`
	Endpoints []catalogEndpoint `json:"endpoints"`
}

type catalogEndpoint struct {
	ID        string `json:"id"`
	Interface string `json:"interface"`
	Region    string `json:"region"`
	RegionID  string `json:"region_id"`
	URL       string `json:"url"`
}

type catalogResponse struct {
	Catalog []catalogService `json:"catalog"`
	Links   catalogLinks     `json:"links"`
}

type catalogLinks struct {
	Self string `json:"self"`
}

func (h handler) catalog(r *http.Request, projectID string) []catalogService {
	services := make([]catalogService, 0, len(h.entries))
	for _, entry := range h.entries {
		path := entry.Path
		if entry.ProjectScoped {
			path += "/" + projectID
		}
		services = append(services, catalogService{
			ID:   entry.ServiceID,
			Type: entry.Type,
			Name: entry.Name,
			Endpoints: []catalogEndpoint{{
				ID:        entry.EndpointID,
				Interface: "public",
				Region:    "RegionOne",
				RegionID:  "RegionOne",
				URL:       h.endpointURL(r, path),
			}},
		})
	}
	return services
}

func (h handler) getCatalog(w http.ResponseWriter, r *http.Request) {
	tokens := r.Header.Values("X-Auth-Token")
	if len(tokens) != 1 || tokens[0] == "" {
		h.writeError(w, r, http.StatusUnauthorized, "A valid authentication token is required.")
		return
	}
	projectID, err := h.service.ProjectForToken(tokens[0])
	if err != nil {
		if errors.Is(err, appidentity.ErrInvalidToken) {
			h.writeError(w, r, http.StatusUnauthorized, "A valid authentication token is required.")
			return
		}
		h.writeError(w, r, http.StatusInternalServerError, "Unable to validate the authentication token.")
		return
	}

	writeJSON(w, r, http.StatusOK, catalogResponse{
		Catalog: h.catalog(r, projectID),
		Links:   catalogLinks{Self: h.endpointURL(r, identityPath+"/auth/catalog")},
	})
}
