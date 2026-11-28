package server

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/go-chi/chi/v5"

	apicompute "github.com/JSYoo5B/sandstack/internal/api/compute"
	apiidentity "github.com/JSYoo5B/sandstack/internal/api/identity"
	apiimage "github.com/JSYoo5B/sandstack/internal/api/image"
	apinetwork "github.com/JSYoo5B/sandstack/internal/api/network"
	apivolume "github.com/JSYoo5B/sandstack/internal/api/volume"
	appcompute "github.com/JSYoo5B/sandstack/internal/app/compute"
	appidentity "github.com/JSYoo5B/sandstack/internal/app/identity"
	appimage "github.com/JSYoo5B/sandstack/internal/app/image"
	appnetwork "github.com/JSYoo5B/sandstack/internal/app/network"
	"github.com/JSYoo5B/sandstack/internal/app/sandbox"
	appvolume "github.com/JSYoo5B/sandstack/internal/app/volume"
	"github.com/JSYoo5B/sandstack/internal/domain/compute"
	"github.com/JSYoo5B/sandstack/internal/domain/identity"
	"github.com/JSYoo5B/sandstack/internal/domain/image"
	"github.com/JSYoo5B/sandstack/internal/domain/network"
	"github.com/JSYoo5B/sandstack/internal/domain/volume"
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
	imageFixture := image.Image{
		ID:              "962a0750-d2e0-4400-88e0-a9ad35758eac",
		Name:            "fixture-image",
		ProjectID:       fixture.Project.ID,
		ContainerFormat: image.ContainerFormatBare,
		DiskFormat:      image.DiskFormatQCOW2,
		CreatedAt:       created,
		UpdatedAt:       created,
	}
	imageService := appimage.New(imageFixture, now, fixtureImageData, randomUUID)
	networkService := appnetwork.New(network.Network{
		ID:        "43d92fea-ce36-4d02-aba4-29806839f902",
		Name:      "fixture-network",
		ProjectID: fixture.Project.ID,
	}, randomUUID)
	volumeService := appvolume.NewWithImageLookup(volume.Volume{
		ID:        "7b37635e-be9b-4bfb-bc2e-d163b75921e6",
		Name:      volume.NullableText{Value: "fixture-volume", Valid: true},
		ProjectID: fixture.Project.ID,
		UserID:    fixture.User.ID,
		Size:      1,
		CreatedAt: created,
		UpdatedAt: created,
	}, now, randomUUID, imageService)
	computeService := appcompute.New(compute.Server{
		ID:        "d8ba25c2-8224-4e09-88ef-fab16c654770",
		Name:      "fixture-server",
		ProjectID: fixture.Project.ID,
		UserID:    fixture.User.ID,
		ImageID:   imageFixture.ID,
		CreatedAt: created,
		UpdatedAt: created,
	}, now, volumeService)
	keyPairs := appcompute.NewKeyPairs(now, rand.Reader)
	journal := sandbox.NewRequestJournal(128)
	state := sandbox.NewState(
		identityService.InvalidateTokens,
		computeService.Reset, keyPairs.Reset, networkService.Reset, imageService.Reset, volumeService.Reset,
		journal.Reset,
	)

	router := chi.NewRouter()
	services := router.With(guardSandboxState(state), recordServiceRequests(journal))
	endpointURL := func(r *http.Request, path string) string {
		return resolveEndpointURL(publicURL, r, path)
	}
	apiidentity.Register(services, identityService, endpointURL, apiidentity.CatalogEntry{
		ServiceID:  "f1746a2a9de3473aa728b42dd5da3c7d",
		EndpointID: "8adbd297bb4a4551a7fe5676a5fece03",
		Type:       "compute",
		Name:       "nova",
		Path:       apicompute.Path + "/" + fixture.Project.ID,
	}, apiidentity.CatalogEntry{
		ServiceID:  "930ac12606054fe897303ea77383770c",
		EndpointID: "6fd74ac16c5246e1bc9a765172237049",
		Type:       "network",
		Name:       "neutron",
		Path:       apinetwork.CatalogPath,
	}, apiidentity.CatalogEntry{
		ServiceID:  "fa5eee918aad4975b49e8ffa7010629f",
		EndpointID: "a2fbad23b2084836bff2ecf5f6ca4ed2",
		Type:       "image",
		Name:       "glance",
		Path:       apiimage.CatalogPath,
	}, apiidentity.CatalogEntry{
		ServiceID:  "2913d077918c4299aac9f4bbdbe7d993",
		EndpointID: "0f336234de5b4d42b7fd18b895c21438",
		Type:       "block-storage",
		Name:       "cinder",
		Path:       apivolume.Path + "/" + fixture.Project.ID,
	})
	apicompute.Register(
		services, computeService, appcompute.NewAttachments(computeService, volumeService), keyPairs,
		identityService.ProjectForToken, identityService.UserForToken, endpointURL,
	)
	apinetwork.Register(services, networkService, identityService.ProjectForToken, endpointURL)
	apiimage.Register(services, imageService, identityService.ProjectForToken, endpointURL)
	apivolume.Register(services, volumeService, identityService.ProjectForToken, endpointURL)
	registerControlRoutes(router, state, config.AdminToken,
		func(r chi.Router) { registerIdentityControls(r, identityService) },
		func(r chi.Router) { registerComputeControls(r, computeService) },
		func(r chi.Router) { registerNetworkControls(r, networkService) },
		func(r chi.Router) { registerImageControls(r, imageService) },
		func(r chi.Router) { registerVolumeControls(r, volumeService) },
		func(r chi.Router) { registerRequestJournal(r, journal) },
	)
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

func randomUUID() string {
	var id [16]byte
	rand.Read(id[:])
	id[6] = (id[6] & 0x0f) | 0x40
	id[8] = (id[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:])
}
