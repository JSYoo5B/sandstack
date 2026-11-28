package identity

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	domainidentity "github.com/JSYoo5B/sandstack/internal/domain/identity"
)

const maxTokenRequestBytes = 64 * 1024

var (
	errInvalidTokenRequest             = errors.New("expected a project-scoped password or application credential request")
	errScopedApplicationCredentialAuth = errors.New("application credentials cannot request a scope")
)

type tokenRequest struct {
	Auth struct {
		Identity struct {
			Methods  []string `json:"methods"`
			Password *struct {
				User struct {
					selectorRequest
					Password string `json:"password"`
				} `json:"user"`
			} `json:"password"`
			ApplicationCredential *struct {
				ID     string           `json:"id"`
				Name   string           `json:"name"`
				Secret string           `json:"secret"`
				User   *selectorRequest `json:"user"`
			} `json:"application_credential"`
		} `json:"identity"`
		Scope *struct {
			Project selectorRequest `json:"project"`
		} `json:"scope"`
	} `json:"auth"`
}

type tokenGrant struct {
	password              *domainidentity.Credentials
	applicationCredential *domainidentity.ApplicationCredentialAuth
}

type selectorRequest struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Domain *struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"domain"`
}

func readTokenRequest(w http.ResponseWriter, r *http.Request) (tokenGrant, error) {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxTokenRequestBytes))
	decoder.DisallowUnknownFields()
	var request tokenRequest
	if err := decoder.Decode(&request); err != nil {
		return tokenGrant{}, err
	}
	if err := decoder.Decode(new(json.RawMessage)); err != io.EOF {
		if err == nil {
			err = errInvalidTokenRequest
		}
		return tokenGrant{}, err
	}

	identity := request.Auth.Identity
	if len(identity.Methods) != 1 {
		return tokenGrant{}, errInvalidTokenRequest
	}
	switch identity.Methods[0] {
	case "password":
		if identity.Password == nil || identity.ApplicationCredential != nil || request.Auth.Scope == nil {
			return tokenGrant{}, errInvalidTokenRequest
		}
		user := identity.Password.User
		project := request.Auth.Scope.Project
		if user.Password == "" || !user.valid() || !project.valid() {
			return tokenGrant{}, errInvalidTokenRequest
		}
		return tokenGrant{password: &domainidentity.Credentials{
			User: user.selector(), Project: project.selector(), Password: user.Password,
		}}, nil
	case "application_credential":
		credential := identity.ApplicationCredential
		if credential == nil || identity.Password != nil {
			return tokenGrant{}, errInvalidTokenRequest
		}
		if request.Auth.Scope != nil {
			return tokenGrant{}, errScopedApplicationCredentialAuth
		}
		auth := domainidentity.ApplicationCredentialAuth{ID: credential.ID, Secret: credential.Secret}
		if auth.ID == "" {
			if credential.Name == "" || credential.User == nil || !credential.User.valid() {
				return tokenGrant{}, errInvalidTokenRequest
			}
			auth.Name = credential.Name
			auth.User = credential.User.selector()
		}
		return tokenGrant{applicationCredential: &auth}, nil
	}
	return tokenGrant{}, errInvalidTokenRequest
}

func (r selectorRequest) valid() bool {
	if r.ID != "" {
		return r.Name == "" && r.Domain == nil
	}
	return r.Name != "" && r.Domain != nil && (r.Domain.ID != "") != (r.Domain.Name != "")
}

func (r selectorRequest) selector() domainidentity.Selector {
	selector := domainidentity.Selector{ID: r.ID, Name: r.Name}
	if r.Domain != nil {
		selector.DomainID = r.Domain.ID
		selector.DomainName = r.Domain.Name
	}
	return selector
}
