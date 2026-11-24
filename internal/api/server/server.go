package server

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	apiidentity "github.com/JSYoo5B/sandstack/internal/api/identity"
	appidentity "github.com/JSYoo5B/sandstack/internal/app/identity"
	"github.com/JSYoo5B/sandstack/internal/domain/identity"
	memoryidentity "github.com/JSYoo5B/sandstack/internal/store/memory/identity"
)

// New assembles an independent in-memory sandbox with the configured fixture.
func New(config Config) (http.Handler, error) {
	if err := config.validate(); err != nil {
		return nil, err
	}
	publicURL, err := publicOrigin(config.PublicURL)
	if err != nil {
		return nil, err
	}

	domain := identity.Domain{ID: "default", Name: "Default"}
	fixture := identity.Fixture{
		User: identity.User{
			ID: "3f6112f0567a4d82b94ecc2e7398cae1", Name: config.Username, Domain: domain,
		},
		Project: identity.Project{
			ID: "b61cd510a075482f8b00e527107a6496", Name: config.ProjectName, Domain: domain,
		},
		Role: identity.Role{ID: "4fdb10632d69421daf2048a40930eccb", Name: "member"},
	}
	service := appidentity.New(
		fixture, config.Password, time.Hour, time.Now, randomID, memoryidentity.New(),
	)

	router := chi.NewRouter()
	apiidentity.Register(router, service, publicURL)
	registerControlRoutes(router, service, config.AdminToken)
	return router, nil
}

func randomID() string {
	var entropy [16]byte
	rand.Read(entropy[:])
	return base64.RawURLEncoding.EncodeToString(entropy[:])
}
