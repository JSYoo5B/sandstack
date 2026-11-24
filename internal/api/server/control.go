package server

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	appcompute "github.com/JSYoo5B/sandstack/internal/app/compute"
	appidentity "github.com/JSYoo5B/sandstack/internal/app/identity"
	"github.com/JSYoo5B/sandstack/internal/app/sandbox"
)

func registerControlRoutes(
	router chi.Router,
	identityService *appidentity.Service,
	computeService *appcompute.Service,
	state *sandbox.State,
	adminToken string,
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
		controls.Post("/identity/invalidate-tokens", func(w http.ResponseWriter, r *http.Request) {
			identityService.InvalidateTokens()
			noContent(w, r)
		})
		controls.Post("/identity/invalidate-token", func(w http.ResponseWriter, r *http.Request) {
			tokens := r.Header.Values("X-Subject-Token")
			if len(tokens) != 1 || strings.TrimSpace(tokens[0]) == "" {
				http.Error(w, "One X-Subject-Token header is required", http.StatusBadRequest)
				return
			}
			identityService.InvalidateToken(tokens[0])
			noContent(w, r)
		})
		registerComputeControls(controls, computeService)
	})
}

func registerComputeControls(router chi.Router, service *appcompute.Service) {
	const serverPath = "/compute/servers/{serverID}"
	registerComputeControl(router, serverPath+"/fault", service.SetFault)
	registerComputeControl(router, serverPath+"/stale-detail", service.SetStaleDetail)
}

func registerComputeControl(router chi.Router, path string, set func(string, bool) error) {
	handle := func(w http.ResponseWriter, r *http.Request) {
		if err := set(chi.URLParam(r, "serverID"), r.Method == http.MethodPut); err != nil {
			http.Error(w, "Server not found", http.StatusNotFound)
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
