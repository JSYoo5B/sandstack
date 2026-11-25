package server

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/JSYoo5B/sandstack/internal/app/sandbox"
)

func recordServiceRequests(journal *sandbox.RequestJournal) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			response := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(response, r)

			route := chi.RouteContext(r.Context()).RoutePattern()
			if route != "" {
				status := response.Status()
				if status == 0 {
					status = http.StatusOK
				}
				journal.Record(r.Method, route, status)
			}
		})
	}
}

type requestRecord struct {
	Sequence uint64 `json:"sequence"`
	Method   string `json:"method"`
	Route    string `json:"route"`
	Status   int    `json:"status"`
}

func registerRequestJournal(router chi.Router, journal *sandbox.RequestJournal) {
	router.Get("/requests", func(w http.ResponseWriter, _ *http.Request) {
		records := journal.Records()
		response := struct {
			Requests []requestRecord `json:"requests"`
		}{Requests: make([]requestRecord, len(records))}
		for i, record := range records {
			response.Requests[i] = requestRecord(record)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(response)
	})
}
