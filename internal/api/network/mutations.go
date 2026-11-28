package network

import (
	"errors"
	"io"
	"mime"
	"net/http"

	"github.com/go-chi/chi/v5"

	app "github.com/JSYoo5B/sandstack/internal/app/network"
)

const maxCreateBody = 64 * 1024

func readCreateBody(w http.ResponseWriter, r *http.Request, resource string) ([]byte, bool) {
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxCreateBody))
	if err != nil {
		var sizeError *http.MaxBytesError
		if errors.As(err, &sizeError) {
			writeError(w, http.StatusRequestEntityTooLarge, "RequestEntityTooLarge", "The "+resource+" request is too large.")
			return nil, false
		}
		writeError(w, http.StatusBadRequest, "BadRequest", "Unable to read the "+resource+" request.")
		return nil, false
	}
	if contentType := r.Header.Get("Content-Type"); len(data) != 0 && contentType != "" {
		mediaType, _, err := mime.ParseMediaType(contentType)
		if err != nil || mediaType != "application/json" {
			writeError(w, http.StatusUnsupportedMediaType, "BadMediaType", "Expected an application/json request.")
			return nil, false
		}
	}
	return data, true
}

func (h handler) createNetwork(w http.ResponseWriter, r *http.Request, projectID string) {
	data, ok := readCreateBody(w, r, "network")
	if !ok {
		return
	}
	params, err := parseCreateNetwork(data)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BadRequest", err.Error())
		return
	}
	if !params.matches(projectID) {
		writeError(w, http.StatusBadRequest, "BadRequest", "Network ownership must match the authenticated project.")
		return
	}
	if params.shared {
		writeError(w, http.StatusForbidden, "PolicyNotAuthorized", "Shared network creation requires administrator privileges.")
		return
	}

	network, err := h.service.Create(projectID, params.name, !params.adminStateUp)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "HTTPInternalServerError", "Unable to create the network.")
		return
	}
	writeJSON(w, http.StatusCreated, networkResponse{Network: describeNetwork(network)})
}

func (h handler) updateNetwork(w http.ResponseWriter, r *http.Request, projectID string) {
	data, ok := readCreateBody(w, r, "network")
	if !ok {
		return
	}
	changes, err := parseUpdateNetwork(data)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BadRequest", err.Error())
		return
	}

	networkID := chi.URLParam(r, "networkID")
	network, err := h.service.Update(projectID, networkID, changes)
	if errors.Is(err, app.ErrNetworkNotFound) {
		writeNetworkNotFound(w, networkID)
		return
	}
	if errors.Is(err, app.ErrNetworkForbidden) {
		writeNetworkForbidden(w, "update")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "HTTPInternalServerError", "Unable to update the network.")
		return
	}
	writeJSON(w, http.StatusOK, networkResponse{Network: describeNetwork(network)})
}

func (h handler) deleteNetwork(w http.ResponseWriter, r *http.Request, projectID string) {
	if !requireEmptyDeleteBody(w, r) {
		return
	}

	networkID := chi.URLParam(r, "networkID")
	err := h.service.Delete(projectID, networkID)
	if errors.Is(err, app.ErrNetworkNotFound) {
		writeNetworkNotFound(w, networkID)
		return
	}
	if errors.Is(err, app.ErrNetworkForbidden) {
		writeNetworkForbidden(w, "delete")
		return
	}
	if errors.Is(err, app.ErrNetworkInUse) {
		writeError(w, http.StatusConflict, "NetworkInUse", "The network still has ports allocated to it.")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "HTTPInternalServerError", "Unable to delete the network.")
		return
	}
	writeNoContent(w)
}
