package network

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	domain "github.com/JSYoo5B/sandstack/internal/domain/network"
)

func changeRouterRoutes(change func(projectID, routerID string, routes []domain.RouterRoute) (domain.Router, error)) projectHandler {
	return func(w http.ResponseWriter, r *http.Request, projectID string) {
		data, ok := readCreateBody(w, r, "router routes")
		if !ok {
			return
		}
		routes, err := parseRouterRouteAction(data)
		if err != nil {
			writeError(w, http.StatusBadRequest, "BadRequest", err.Error())
			return
		}
		routerID := chi.URLParam(r, "routerID")
		router, err := change(projectID, routerID, routes)
		if err != nil {
			writeRouterError(w, routerID, err)
			return
		}
		writeJSON(w, http.StatusOK, routerResponse{Router: describeRouter(router)})
	}
}
