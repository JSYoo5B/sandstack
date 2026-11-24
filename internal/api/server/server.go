package server

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"net/url"
	"time"

	"github.com/go-chi/chi/v5"

	apicompute "github.com/JSYoo5B/sandstack/internal/api/compute"
	apiidentity "github.com/JSYoo5B/sandstack/internal/api/identity"
	appcompute "github.com/JSYoo5B/sandstack/internal/app/compute"
	appidentity "github.com/JSYoo5B/sandstack/internal/app/identity"
	"github.com/JSYoo5B/sandstack/internal/app/sandbox"
	"github.com/JSYoo5B/sandstack/internal/domain/compute"
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
	now := time.Now
	identityService := appidentity.New(
		fixture, config.Password, time.Hour, now, randomID, memoryidentity.New(),
	)
	created := now().UTC()
	computeService := appcompute.New(compute.Server{
		ID:        "d8ba25c2-8224-4e09-88ef-fab16c654770",
		Name:      "fixture-server",
		ProjectID: fixture.Project.ID,
		UserID:    fixture.User.ID,
		CreatedAt: created,
		UpdatedAt: created,
	}, now)
	state := sandbox.NewState(identityService.InvalidateTokens, computeService.Reset)

	router := chi.NewRouter()
	services := router.With(guardSandboxState(state))
	endpointURL := func(r *http.Request, path string) string {
		return resolveEndpointURL(publicURL, r, path)
	}
	apiidentity.Register(services, identityService, endpointURL, apiidentity.CatalogEntry{
		ServiceID:  "f1746a2a9de3473aa728b42dd5da3c7d",
		EndpointID: "8adbd297bb4a4551a7fe5676a5fece03",
		Type:       "compute",
		Name:       "nova",
		Path:       apicompute.Path + "/" + fixture.Project.ID,
	})
	apicompute.Register(services, computeService, identityService.ProjectForToken, endpointURL)
	registerControlRoutes(router, identityService, computeService, state, config.AdminToken)
	return router, nil
}

func resolveEndpointURL(origin *url.URL, r *http.Request, path string) string {
	if origin != nil {
		return origin.ResolveReference(&url.URL{Path: path}).String()
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return (&url.URL{Scheme: scheme, Host: r.Host, Path: path}).String()
}

func randomID() string {
	var entropy [16]byte
	rand.Read(entropy[:])
	return base64.RawURLEncoding.EncodeToString(entropy[:])
}
