package server

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	appcompute "github.com/JSYoo5B/sandstack/internal/app/compute"
	appidentity "github.com/JSYoo5B/sandstack/internal/app/identity"
	appimage "github.com/JSYoo5B/sandstack/internal/app/image"
	appnetwork "github.com/JSYoo5B/sandstack/internal/app/network"
	"github.com/JSYoo5B/sandstack/internal/app/sandbox"
	appvolume "github.com/JSYoo5B/sandstack/internal/app/volume"
)

func registerControlRoutes(
	router chi.Router,
	state *sandbox.State,
	adminToken string,
	register ...func(chi.Router),
) {
	router.Route("/_sandstack", func(router chi.Router) {
		router.Get("/health", noContent)
		router.Get("/ready", noContent)

		controls := router.With(requireAdminToken(adminToken))
		controls.Post("/reset", func(w http.ResponseWriter, r *http.Request) {
			state.Reset()
			noContent(w, r)
		})
		controls = controls.With(guardSandboxState(state))
		for _, route := range register {
			route(controls)
		}
	})
}

func registerIdentityControls(router chi.Router, service *appidentity.Service) {
	router.Post("/identity/invalidate-tokens", func(w http.ResponseWriter, r *http.Request) {
		service.InvalidateTokens()
		noContent(w, r)
	})
	router.Post("/identity/invalidate-token", func(w http.ResponseWriter, r *http.Request) {
		tokens := r.Header.Values("X-Subject-Token")
		if len(tokens) != 1 || strings.TrimSpace(tokens[0]) == "" {
			http.Error(w, "One X-Subject-Token header is required", http.StatusBadRequest)
			return
		}
		service.InvalidateToken(tokens[0])
		noContent(w, r)
	})
}

func registerComputeControls(router chi.Router, service *appcompute.Service) {
	const serverPath = "/compute/servers/{resourceID}"
	registerFixtureControl(router, serverPath+"/fault", "Server not found", service.SetFault)
	registerFixtureControl(router, serverPath+"/stale-detail", "Server not found", service.SetStaleDetail)
	registerFixtureControl(router, serverPath+"/unavailable-detail", "Server not found", service.SetUnavailableDetail)
}

func registerNetworkControls(router chi.Router, service *appnetwork.Service) {
	registerFixtureControl(router, "/network/networks/{resourceID}/fault", "Network not found", service.SetDown)
}

func registerImageControls(router chi.Router, service *appimage.Service) {
	registerFixtureControl(router, "/image/images/{resourceID}/deactivated", "Image not found", service.SetDeactivated)
}

func registerVolumeControls(router chi.Router, service *appvolume.Service) {
	registerFixtureControl(router, "/volume/volumes/{resourceID}/fault", "Volume not found", service.SetFailed)
}

func registerFixtureControl(router chi.Router, path, notFound string, set func(string, bool) error) {
	handle := func(w http.ResponseWriter, r *http.Request) {
		if err := set(chi.URLParam(r, "resourceID"), r.Method == http.MethodPut); err != nil {
			http.Error(w, notFound, http.StatusNotFound)
			return
		}
		noContent(w, r)
	}
	router.Put(path, handle)
	router.Delete(path, handle)
}

func requireAdminToken(adminToken string) func(http.Handler) http.Handler {
	expected := sha256.Sum256([]byte(adminToken))
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !validAdminToken(r.Header.Values("X-SandStack-Admin-Token"), expected) {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func validAdminToken(values []string, expected [sha256.Size]byte) bool {
	if len(values) != 1 {
		return false
	}
	provided := sha256.Sum256([]byte(values[0]))
	return subtle.ConstantTimeCompare(provided[:], expected[:]) == 1
}

func noContent(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}
