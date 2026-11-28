package server

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"math/big"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
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
		memoryidentity.NewApplicationCredentials(), rand.Reader,
	)
	for _, additional := range config.Fixtures {
		identityService.WithFixture(identity.Fixture{
			User: identity.User{
				ID: fixtureID("user", additional.Username), Name: additional.Username, Domain: domain,
			},
			Project: identity.Project{
				ID: fixtureID("project", additional.ProjectName), Name: additional.ProjectName, Domain: domain,
			},
			Role: fixture.Role,
		}, additional.Password)
	}
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
	networkService := appnetwork.NewWithExternalNetwork(network.Network{
		ID:        "43d92fea-ce36-4d02-aba4-29806839f902",
		Name:      "fixture-network",
		ProjectID: fixture.Project.ID,
	}, network.Network{
		ID:        "6c5a1e4f-0d3b-4f0e-9a76-2b8f1c3d9e10",
		Name:      "public",
		ProjectID: "d0c1b2a39e8f4c7d8b6a5f4e3d2c1b0a",
	}, network.Subnet{
		ID:      "9e7d3c2b-1a0f-4e8d-b7c6-5a4f3e2d1c0b",
		Name:    "public-subnet",
		CIDR:    netip.MustParsePrefix("203.0.113.0/24"),
		Gateway: netip.MustParseAddr("203.0.113.1"),
		AllocationPools: []network.AllocationPool{{
			Start: netip.MustParseAddr("203.0.113.2"),
			End:   netip.MustParseAddr("203.0.113.254"),
		}},
	}, randomUUID)
	volumeService := appvolume.NewWithImageLookup(volume.Volume{
		ID:        "7b37635e-be9b-4bfb-bc2e-d163b75921e6",
		Name:      volume.NullableText{Value: "fixture-volume", Valid: true},
		ProjectID: fixture.Project.ID,
		UserID:    fixture.User.ID,
		Size:      1,
		CreatedAt: created,
		UpdatedAt: created,
	}, now, randomUUID, imageService).
		WithTransferKeys(randomTransferKey).
		WithImageUploads(imageService)
	serverFixture := compute.Server{
		ID:        "d8ba25c2-8224-4e09-88ef-fab16c654770",
		Name:      "fixture-server",
		ProjectID: fixture.Project.ID,
		UserID:    fixture.User.ID,
		ImageID:   imageFixture.ID,
		FlavorID:  appcompute.FixtureFlavorID,
		CreatedAt: created,
		UpdatedAt: created,
	}
	computeService := appcompute.New(serverFixture, now, volumeService, compute.Action{
		Name:      compute.ActionCreate,
		ServerID:  serverFixture.ID,
		RequestID: computeRequestID(),
		ProjectID: serverFixture.ProjectID,
		UserID:    serverFixture.UserID,
		StartTime: created,
	})
	keyPairs := appcompute.NewKeyPairs(now, rand.Reader)
	journal := sandbox.NewRequestJournal(128)
	state := sandbox.NewState(
		identityService.Reset,
		computeService.Reset, keyPairs.Reset, networkService.Reset, imageService.Reset, volumeService.Reset,
		journal.Reset,
	)

	router := chi.NewRouter()
	services := router.With(guardSandboxState(state), recordServiceRequests(journal))
	endpointURL := func(r *http.Request, path string) string {
		return resolveEndpointURL(publicURL, r, path)
	}
	apiidentity.Register(services, identityService, endpointURL, apiidentity.CatalogEntry{
		ServiceID:     "f1746a2a9de3473aa728b42dd5da3c7d",
		EndpointID:    "8adbd297bb4a4551a7fe5676a5fece03",
		Type:          "compute",
		Name:          "nova",
		Path:          apicompute.Path,
		ProjectScoped: true,
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
		ServiceID:     "2913d077918c4299aac9f4bbdbe7d993",
		EndpointID:    "0f336234de5b4d42b7fd18b895c21438",
		Type:          "block-storage",
		Name:          "cinder",
		Path:          apivolume.Path,
		ProjectScoped: true,
	})
	servers := appcompute.NewServers(
		computeService, imageService, networkService, volumeService, keyPairs, randomUUID, randomPassword,
	)
	apicompute.Register(
		services, computeService, servers, appcompute.NewAttachments(computeService, volumeService), keyPairs,
		appcompute.NewServerGroups(computeService, randomUUID),
		identityService.ProjectForToken, identityService.UserForToken, endpointURL, computeRequestID,
	)
	apinetwork.Register(services, networkService, identityService.ProjectForToken, endpointURL)
	apiimage.Register(services, imageService, identityService.ProjectForToken, endpointURL)
	apivolume.Register(services, volumeService, identityService.ProjectForToken, endpointURL)
	registerControlRoutes(router, state, config.AdminToken,
		func(r chi.Router) { registerIdentityControls(r, identityService) },
		func(r chi.Router) { registerComputeControls(r, computeService, servers) },
		func(r chi.Router) { registerNetworkControls(r, networkService) },
		func(r chi.Router) { registerImageControls(r, imageService) },
		func(r chi.Router) { registerVolumeControls(r, volumeService) },
		func(r chi.Router) { registerRequestJournal(r, journal) },
	)
	return router, nil
}

func fixtureID(kind, name string) string {
	digest := sha256.Sum256([]byte("sandstack-fixture-" + kind + "\x00" + name))
	return hex.EncodeToString(digest[:16])
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

// passwordSymbols are the groups Nova draws generated admin passwords from.
var passwordSymbols = []string{"23456789", "ABCDEFGHJKLMNPQRSTUVWXYZ", "abcdefghijkmnopqrstuvwxyz"}

const passwordLength = 12

func randomPassword() string {
	symbols := strings.Join(passwordSymbols, "")
	password := make([]byte, 0, passwordLength)
	for _, group := range passwordSymbols {
		password = append(password, group[randomIndex(len(group))])
	}
	for len(password) < passwordLength {
		password = append(password, symbols[randomIndex(len(symbols))])
	}
	for i := len(password) - 1; i > 0; i-- {
		j := randomIndex(i + 1)
		password[i], password[j] = password[j], password[i]
	}
	return string(password)
}

func randomIndex(n int) int {
	value, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		return 0
	}
	return int(value.Int64())
}

const transferKeyLength = 16

func randomTransferKey() string {
	var entropy [transferKeyLength / 2]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return ""
	}
	return hex.EncodeToString(entropy[:])
}

func computeRequestID() string {
	return "req-" + randomUUID()
}

func randomUUID() string {
	var id [16]byte
	rand.Read(id[:])
	id[6] = (id[6] & 0x0f) | 0x40
	id[8] = (id[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:])
}
