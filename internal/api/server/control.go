package server

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"

	"github.com/go-chi/chi/v5"

	appidentity "github.com/JSYoo5B/sandstack/internal/app/identity"
)

func registerControlRoutes(router chi.Router, service *appidentity.Service, adminToken string) {
	expected := sha256.Sum256([]byte(adminToken))

	router.Route("/_sandstack", func(router chi.Router) {
		router.Get("/health", noContent)
		router.Get("/ready", noContent)
		router.Post("/identity/invalidate-tokens", func(w http.ResponseWriter, r *http.Request) {
			if !validAdminToken(r.Header.Values("X-SandStack-Admin-Token"), expected) {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			service.InvalidateTokens()
			noContent(w, r)
		})
	})
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
