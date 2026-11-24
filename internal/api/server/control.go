package server

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	appcompute "github.com/JSYoo5B/sandstack/internal/app/compute"
	appidentity "github.com/JSYoo5B/sandstack/internal/app/identity"
)

func registerControlRoutes(
	router chi.Router,
	identityService *appidentity.Service,
	computeService *appcompute.Service,
	adminToken string,
) {
	router.Route("/_sandstack", func(router chi.Router) {
		router.Get("/health", noContent)
		router.Get("/ready", noContent)

		controls := router.With(requireAdminToken(adminToken))
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
	const faultPath = "/compute/servers/{serverID}/fault"
	handleFault := func(w http.ResponseWriter, r *http.Request) {
		if err := service.SetFault(chi.URLParam(r, "serverID"), r.Method == http.MethodPut); err != nil {
			http.Error(w, "Server not found", http.StatusNotFound)
			return
		}
		noContent(w, r)
	}
	router.Put(faultPath, handleFault)
	router.Delete(faultPath, handleFault)
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
