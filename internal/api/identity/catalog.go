package identity

import (
	"errors"
	"net/http"

	appidentity "github.com/JSYoo5B/sandstack/internal/app/identity"
)

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

func (h handler) catalog(r *http.Request) []catalogService {
	return []catalogService{{
		ID:   "f67f532d68cc4616bc5b81a6eb84ab7f",
		Type: "identity",
		Name: "keystone",
		Endpoints: []catalogEndpoint{{
			ID:        "9a077ab4b7f0463b95f1baccde36b964",
			Interface: "public",
			Region:    "RegionOne",
			RegionID:  "RegionOne",
			URL:       h.endpointURL(r, identityPath),
		}},
	}}
}

func (h handler) getCatalog(w http.ResponseWriter, r *http.Request) {
	tokens := r.Header.Values("X-Auth-Token")
	if len(tokens) != 1 || tokens[0] == "" {
		h.writeError(w, r, http.StatusUnauthorized, "A valid authentication token is required.")
		return
	}
	if err := h.service.Validate(tokens[0]); err != nil {
		if errors.Is(err, appidentity.ErrInvalidToken) {
			h.writeError(w, r, http.StatusUnauthorized, "A valid authentication token is required.")
			return
		}
		h.writeError(w, r, http.StatusInternalServerError, "Unable to validate the authentication token.")
		return
	}

	writeJSON(w, r, http.StatusOK, catalogResponse{
		Catalog: h.catalog(r),
		Links:   catalogLinks{Self: h.endpointURL(r, identityPath+"/auth/catalog")},
	})
}
