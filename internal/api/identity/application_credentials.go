package identity

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	appidentity "github.com/JSYoo5B/sandstack/internal/app/identity"
	domainidentity "github.com/JSYoo5B/sandstack/internal/domain/identity"
)

const (
	maxApplicationCredentialRequestBytes = 64 * 1024
	applicationCredentialTimeLayout      = "2006-01-02T15:04:05.000000"
)

var (
	errInvalidApplicationCredentialRequest = errors.New("invalid application credential request")
	roleIDPattern                          = regexp.MustCompile(`^[a-zA-Z0-9-]+$`)
)

type applicationCredentialRequest struct {
	ApplicationCredential *struct {
		ID           *string           `json:"id"`
		Name         *string           `json:"name"`
		Description  *string           `json:"description"`
		Secret       *string           `json:"secret"`
		ExpiresAt    *string           `json:"expires_at"`
		ProjectID    *string           `json:"project_id"`
		Unrestricted *bool             `json:"unrestricted"`
		System       *string           `json:"system"`
		Roles        []roleSelector    `json:"roles"`
		AccessRules  []json.RawMessage `json:"access_rules"`
	} `json:"application_credential"`
}

type roleSelector struct {
	ID   *string `json:"id"`
	Name *string `json:"name"`
}

type applicationCredentialBody struct {
	ID           string           `json:"id"`
	Name         string           `json:"name"`
	Description  *string          `json:"description"`
	ExpiresAt    *string          `json:"expires_at"`
	ProjectID    string           `json:"project_id"`
	Roles        []credentialRole `json:"roles"`
	Unrestricted *bool            `json:"unrestricted"`
	Links        credentialLinks  `json:"links"`
}

type createdApplicationCredential struct {
	applicationCredentialBody
	Secret string `json:"secret"`
}

type storedApplicationCredential struct {
	applicationCredentialBody
	UserID string  `json:"user_id"`
	System *string `json:"system"`
}

type credentialRole struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	DomainID *string `json:"domain_id"`
}

type credentialLinks struct {
	Self string `json:"self"`
}

type applicationCredentialListLinks struct {
	Self     string  `json:"self"`
	Previous *string `json:"previous"`
	Next     *string `json:"next"`
}

func (h handler) applicationCredentialsPath(userID string) string {
	return identityPath + "/users/" + userID + "/application_credentials"
}

func (h handler) createApplicationCredential(w http.ResponseWriter, r *http.Request) {
	token, ok := h.requireToken(w, r)
	if !ok {
		return
	}
	request, err := readApplicationCredentialRequest(w, r)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			h.writeError(w, r, http.StatusRequestEntityTooLarge, "The application credential request is too large.")
			return
		}
		h.writeError(w, r, http.StatusBadRequest, "Invalid application credential request.")
		return
	}

	userID := chi.URLParam(r, "userID")
	credential, secret, err := h.service.CreateApplicationCredential(token, userID, request)
	if err != nil {
		h.writeApplicationCredentialError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusCreated, map[string]createdApplicationCredential{
		"application_credential": {
			applicationCredentialBody: h.applicationCredentialBody(r, credential),
			Secret:                    secret,
		},
	})
}

func (h handler) listApplicationCredentials(w http.ResponseWriter, r *http.Request) {
	token, ok := h.requireToken(w, r)
	if !ok {
		return
	}
	userID := chi.URLParam(r, "userID")
	credentials, err := h.service.ListApplicationCredentials(token, userID, r.URL.Query().Get("name"))
	if err != nil {
		h.writeApplicationCredentialError(w, r, err)
		return
	}

	bodies := make([]storedApplicationCredential, 0, len(credentials))
	for _, credential := range credentials {
		bodies = append(bodies, h.storedApplicationCredential(r, credential))
	}
	writeJSON(w, r, http.StatusOK, struct {
		ApplicationCredentials []storedApplicationCredential  `json:"application_credentials"`
		Links                  applicationCredentialListLinks `json:"links"`
	}{
		ApplicationCredentials: bodies,
		Links:                  applicationCredentialListLinks{Self: h.endpointURL(r, h.applicationCredentialsPath(userID))},
	})
}

func (h handler) getApplicationCredential(w http.ResponseWriter, r *http.Request) {
	token, ok := h.requireToken(w, r)
	if !ok {
		return
	}
	credential, err := h.service.GetApplicationCredential(
		token, chi.URLParam(r, "userID"), chi.URLParam(r, "credentialID"),
	)
	if err != nil {
		h.writeApplicationCredentialError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]storedApplicationCredential{
		"application_credential": h.storedApplicationCredential(r, credential),
	})
}

func (h handler) deleteApplicationCredential(w http.ResponseWriter, r *http.Request) {
	token, ok := h.requireToken(w, r)
	if !ok {
		return
	}
	err := h.service.DeleteApplicationCredential(token, chi.URLParam(r, "userID"), chi.URLParam(r, "credentialID"))
	if err != nil {
		h.writeApplicationCredentialError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h handler) requireToken(w http.ResponseWriter, r *http.Request) (string, bool) {
	tokens := r.Header.Values("X-Auth-Token")
	if len(tokens) != 1 || tokens[0] == "" {
		h.writeError(w, r, http.StatusUnauthorized, "A valid authentication token is required.")
		return "", false
	}
	if err := h.service.Validate(tokens[0]); err != nil {
		h.writeApplicationCredentialError(w, r, err)
		return "", false
	}
	return tokens[0], true
}

func (h handler) writeApplicationCredentialError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, appidentity.ErrInvalidToken):
		h.writeError(w, r, http.StatusUnauthorized, "A valid authentication token is required.")
	case errors.Is(err, appidentity.ErrForbidden):
		h.writeError(w, r, http.StatusForbidden, "You are not authorized to perform the requested action.")
	case errors.Is(err, appidentity.ErrApplicationCredentialNotFound):
		h.writeError(w, r, http.StatusNotFound, "Could not find Application Credential: "+
			chi.URLParam(r, "credentialID")+".")
	case errors.Is(err, appidentity.ErrRoleNotFound):
		h.writeError(w, r, http.StatusNotFound, "Could not find role.")
	case errors.Is(err, appidentity.ErrInvalidApplicationCredential):
		h.writeError(w, r, http.StatusBadRequest, "Invalid application credential request.")
	case errors.Is(err, appidentity.ErrApplicationCredentialExists):
		h.writeError(w, r, http.StatusConflict,
			"Conflict occurred attempting to store application_credential - Duplicate entry.")
	default:
		h.writeError(w, r, http.StatusInternalServerError, "Unable to process the application credential.")
	}
}

func (h handler) applicationCredentialBody(
	r *http.Request, credential domainidentity.ApplicationCredential,
) applicationCredentialBody {
	roles := make([]credentialRole, 0, len(credential.Roles))
	for _, role := range credential.Roles {
		roles = append(roles, credentialRole{ID: role.ID, Name: role.Name})
	}
	var expiresAt *string
	if credential.ExpiresAt != nil {
		formatted := credential.ExpiresAt.UTC().Format(applicationCredentialTimeLayout)
		expiresAt = &formatted
	}
	return applicationCredentialBody{
		ID:           credential.ID,
		Name:         credential.Name,
		Description:  credential.Description,
		ExpiresAt:    expiresAt,
		ProjectID:    credential.ProjectID,
		Roles:        roles,
		Unrestricted: credential.Unrestricted,
		Links: credentialLinks{
			Self: h.endpointURL(r, h.applicationCredentialsPath(credential.UserID)+"/"+credential.ID),
		},
	}
}

func (h handler) storedApplicationCredential(
	r *http.Request, credential domainidentity.ApplicationCredential,
) storedApplicationCredential {
	return storedApplicationCredential{
		applicationCredentialBody: h.applicationCredentialBody(r, credential),
		UserID:                    credential.UserID,
	}
}

func readApplicationCredentialRequest(
	w http.ResponseWriter, r *http.Request,
) (domainidentity.ApplicationCredentialRequest, error) {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxApplicationCredentialRequestBytes))
	decoder.DisallowUnknownFields()
	var request applicationCredentialRequest
	if err := decoder.Decode(&request); err != nil {
		return domainidentity.ApplicationCredentialRequest{}, err
	}
	if err := decoder.Decode(new(json.RawMessage)); err != io.EOF {
		if err == nil {
			err = errInvalidApplicationCredentialRequest
		}
		return domainidentity.ApplicationCredentialRequest{}, err
	}

	body := request.ApplicationCredential
	if body == nil || body.Name == nil || !validName(*body.Name) ||
		(body.ID != nil && *body.ID == "") || body.System != nil || len(body.AccessRules) > 0 {
		return domainidentity.ApplicationCredentialRequest{}, errInvalidApplicationCredentialRequest
	}
	parsed := domainidentity.ApplicationCredentialRequest{
		Name:         *body.Name,
		Description:  body.Description,
		Unrestricted: body.Unrestricted,
	}
	if body.Secret != nil {
		parsed.Secret = *body.Secret
	}
	for _, role := range body.Roles {
		switch {
		case role.ID != nil && role.Name == nil && len(*role.ID) <= 64 && roleIDPattern.MatchString(*role.ID):
			parsed.Roles = append(parsed.Roles, domainidentity.Selector{ID: *role.ID})
		case role.Name != nil && role.ID == nil && validName(*role.Name):
			parsed.Roles = append(parsed.Roles, domainidentity.Selector{Name: *role.Name})
		default:
			return domainidentity.ApplicationCredentialRequest{}, errInvalidApplicationCredentialRequest
		}
	}
	if body.ExpiresAt != nil && *body.ExpiresAt != "" {
		expiresAt, err := parseExpiration(*body.ExpiresAt)
		if err != nil {
			return domainidentity.ApplicationCredentialRequest{}, errInvalidApplicationCredentialRequest
		}
		parsed.ExpiresAt = &expiresAt
	}
	return parsed, nil
}

func validName(name string) bool {
	length := utf8.RuneCountInString(name)
	return length >= 1 && length <= 255 && strings.TrimSpace(name) != ""
}

func parseExpiration(value string) (time.Time, error) {
	if len(value) > 10 && value[10] == ' ' {
		value = value[:10] + "T" + value[11:]
	}
	if !strings.HasSuffix(value, "Z") {
		value += "Z"
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, value)
	return expiresAt.UTC(), err
}
