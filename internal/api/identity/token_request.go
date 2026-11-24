package identity

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	domainidentity "github.com/JSYoo5B/sandstack/internal/domain/identity"
)

const maxTokenRequestBytes = 64 * 1024

var errInvalidTokenRequest = errors.New("expected a project-scoped password authentication request")

type tokenRequest struct {
	Auth struct {
		Identity struct {
			Methods  []string `json:"methods"`
			Password struct {
				User struct {
					selectorRequest
					Password string `json:"password"`
				} `json:"user"`
			} `json:"password"`
		} `json:"identity"`
		Scope struct {
			Project selectorRequest `json:"project"`
		} `json:"scope"`
	} `json:"auth"`
}

type selectorRequest struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Domain *struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"domain"`
}

func readTokenRequest(w http.ResponseWriter, r *http.Request) (domainidentity.Credentials, error) {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxTokenRequestBytes))
	decoder.DisallowUnknownFields()
	var request tokenRequest
	if err := decoder.Decode(&request); err != nil {
		return domainidentity.Credentials{}, err
	}
	if err := decoder.Decode(new(json.RawMessage)); err != io.EOF {
		if err == nil {
			err = errInvalidTokenRequest
		}
		return domainidentity.Credentials{}, err
	}

	identity := request.Auth.Identity
	user := identity.Password.User
	project := request.Auth.Scope.Project
	if len(identity.Methods) != 1 || identity.Methods[0] != "password" || user.Password == "" ||
		!user.valid() || !project.valid() {
		return domainidentity.Credentials{}, errInvalidTokenRequest
	}
	return domainidentity.Credentials{
		User: user.selector(), Project: project.selector(), Password: user.Password,
	}, nil
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
