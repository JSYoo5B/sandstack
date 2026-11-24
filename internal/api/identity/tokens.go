package identity

import (
	"errors"
	"mime"
	"net/http"
	"time"

	appidentity "github.com/JSYoo5B/sandstack/internal/app/identity"
)

type tokenResponse struct {
	Token tokenBody `json:"token"`
}

type tokenBody struct {
	AuditIDs  []string         `json:"audit_ids"`
	Methods   []string         `json:"methods"`
	IssuedAt  time.Time        `json:"issued_at"`
	ExpiresAt time.Time        `json:"expires_at"`
	User      tokenUser        `json:"user"`
	Project   scopedResource   `json:"project"`
	Roles     []namedResource  `json:"roles"`
	Catalog   []catalogService `json:"catalog,omitempty"`
}

type namedResource struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type scopedResource struct {
	namedResource
	Domain namedResource `json:"domain"`
}

type tokenUser struct {
	scopedResource
	PasswordExpiresAt *time.Time `json:"password_expires_at"`
}

func (h handler) issueToken(w http.ResponseWriter, r *http.Request) {
	if contentType := r.Header.Get("Content-Type"); contentType != "" {
		mediaType, _, err := mime.ParseMediaType(contentType)
		if err != nil || mediaType != "application/json" {
			h.writeError(w, r, http.StatusUnsupportedMediaType, "Expected an application/json request.")
			return
		}
	}
	credentials, err := readTokenRequest(w, r)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			h.writeError(w, r, http.StatusRequestEntityTooLarge, "The authentication request is too large.")
			return
		}
		h.writeError(w, r, http.StatusBadRequest, "Expected a project-scoped password authentication request.")
		return
	}

	token, err := h.service.Authenticate(credentials)
	if err != nil {
		if errors.Is(err, appidentity.ErrInvalidCredentials) {
			h.writeError(w, r, http.StatusUnauthorized, "Invalid authentication credentials or project scope.")
			return
		}
		h.writeError(w, r, http.StatusInternalServerError, "Unable to issue an authentication token.")
		return
	}

	user, project, role := token.Fixture.User, token.Fixture.Project, token.Fixture.Role
	response := tokenResponse{Token: tokenBody{
		AuditIDs:  []string{token.AuditID},
		Methods:   []string{"password"},
		IssuedAt:  token.IssuedAt.UTC(),
		ExpiresAt: token.ExpiresAt.UTC(),
		User: tokenUser{scopedResource: scopedResource{
			namedResource: namedResource{ID: user.ID, Name: user.Name},
			Domain:        namedResource{ID: user.Domain.ID, Name: user.Domain.Name},
		}},
		Project: scopedResource{
			namedResource: namedResource{ID: project.ID, Name: project.Name},
			Domain:        namedResource{ID: project.Domain.ID, Name: project.Domain.Name},
		},
		Roles: []namedResource{{ID: role.ID, Name: role.Name}},
	}}
	if !r.URL.Query().Has("nocatalog") {
		response.Token.Catalog = h.catalog(r)
	}

	w.Header().Set("X-Subject-Token", token.ID)
	writeJSON(w, r, http.StatusCreated, response)
}
