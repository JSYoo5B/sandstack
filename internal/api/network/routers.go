package network

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/go-chi/chi/v5"

	app "github.com/JSYoo5B/sandstack/internal/app/network"
	domain "github.com/JSYoo5B/sandstack/internal/domain/network"
)

type routerBody struct {
	ID                  string            `json:"id"`
	Name                string            `json:"name"`
	Description         string            `json:"description"`
	ProjectID           string            `json:"project_id"`
	TenantID            string            `json:"tenant_id"`
	AdminStateUp        bool              `json:"admin_state_up"`
	Status              string            `json:"status"`
	ExternalGatewayInfo *struct{}         `json:"external_gateway_info"`
	GatewayPortID       *string           `json:"gw_port_id"`
	Routes              []routerRouteBody `json:"routes"`
}

type routerRouteBody struct {
	Destination string `json:"destination"`
	NextHop     string `json:"nexthop"`
}

type routerResponse struct {
	Router routerBody `json:"router"`
}

func describeRouter(router domain.Router) routerBody {
	routes := make([]routerRouteBody, len(router.Routes))
	for i, route := range router.Routes {
		routes[i] = routerRouteBody{Destination: route.Destination.String(), NextHop: route.NextHop.String()}
	}
	return routerBody{
		ID:           router.ID,
		Name:         router.Name,
		Description:  router.Description,
		ProjectID:    router.ProjectID,
		TenantID:     router.ProjectID,
		AdminStateUp: router.AdminStateUp,
		Status:       "ACTIVE",
		Routes:       routes,
	}
}

func (h handler) createRouter(w http.ResponseWriter, r *http.Request, projectID string) {
	data, ok := readCreateBody(w, r, "router")
	if !ok {
		return
	}
	params, err := parseCreateRouter(data)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BadRequest", err.Error())
		return
	}
	if !params.matches(projectID) {
		writeError(w, http.StatusBadRequest, "BadRequest", "Router ownership must match the authenticated project.")
		return
	}
	router, err := h.service.CreateRouter(projectID, params.name, params.description, params.adminStateUp)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "HTTPInternalServerError", "Unable to create the router.")
		return
	}
	writeJSON(w, http.StatusCreated, routerResponse{Router: describeRouter(router)})
}

func (h handler) listRouters(w http.ResponseWriter, r *http.Request, projectID string) {
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BadRequest", "the router list query is malformed")
		return
	}
	filter, err := parseRouterListQuery(query)
	if err != nil {
		kind := "BadRequest"
		if errors.Is(err, errRouterListAdminState) {
			kind = "InvalidInput"
		}
		writeError(w, http.StatusBadRequest, kind, err.Error())
		return
	}
	options, err := parsePageOptions(query)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BadRequest", err.Error())
		return
	}
	page, err := h.service.ListRoutersPage(projectID, filter, options)
	if err != nil {
		writeRouterError(w, options.Marker, err)
		return
	}
	response := struct {
		Routers []routerBody `json:"routers"`
		Links   []pageLink   `json:"routers_links,omitempty"`
	}{
		Routers: make([]routerBody, len(page.Items)),
		Links:   h.pageLinks(r, Path+"/routers", query, page.NextMarker, page.PreviousMarker),
	}
	for i, router := range page.Items {
		response.Routers[i] = describeRouter(router)
	}
	writeJSON(w, http.StatusOK, response)
}

func (h handler) showRouter(w http.ResponseWriter, r *http.Request, projectID string) {
	routerID := chi.URLParam(r, "routerID")
	router, err := h.service.GetRouter(projectID, routerID)
	if err != nil {
		writeRouterError(w, routerID, err)
		return
	}
	writeJSON(w, http.StatusOK, routerResponse{Router: describeRouter(router)})
}

func (h handler) updateRouter(w http.ResponseWriter, r *http.Request, projectID string) {
	data, ok := readCreateBody(w, r, "router")
	if !ok {
		return
	}
	var changes app.RouterUpdate
	if err := parseRouterFields(data, &changes, nil); err != nil {
		writeError(w, http.StatusBadRequest, "BadRequest", err.Error())
		return
	}
	routerID := chi.URLParam(r, "routerID")
	router, err := h.service.UpdateRouter(projectID, routerID, changes)
	if err != nil {
		writeRouterError(w, routerID, err)
		return
	}
	writeJSON(w, http.StatusOK, routerResponse{Router: describeRouter(router)})
}

func (h handler) deleteRouter(w http.ResponseWriter, r *http.Request, projectID string) {
	if !requireEmptyDeleteBody(w, r) {
		return
	}
	routerID := chi.URLParam(r, "routerID")
	if err := h.service.DeleteRouter(projectID, routerID); err != nil {
		writeRouterError(w, routerID, err)
		return
	}
	writeNoContent(w)
}

func writeRouterError(w http.ResponseWriter, routerID string, err error) {
	if errors.Is(err, app.ErrRouterNotFound) {
		writeError(w, http.StatusNotFound, "RouterNotFound", fmt.Sprintf("Router %s could not be found.", routerID))
		return
	}
	if errors.Is(err, app.ErrRouterInUse) {
		writeError(w, http.StatusConflict, "RouterInUse", "Remove the router interfaces before deleting the router.")
		return
	}
	if errors.Is(err, app.ErrInvalidRouterRoutes) {
		writeError(w, http.StatusBadRequest, "InvalidRoutes", err.Error())
		return
	}
	writeError(w, http.StatusInternalServerError, "HTTPInternalServerError", "Unable to process the router request.")
}
