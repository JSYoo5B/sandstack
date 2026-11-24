package server

import (
	"net/http"

	"github.com/JSYoo5B/sandstack/internal/app/sandbox"
)

func guardSandboxState(state *sandbox.State) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			state.Run(func() { next.ServeHTTP(w, r) })
		})
	}
}
