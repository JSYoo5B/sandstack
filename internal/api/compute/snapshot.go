package compute

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"

	"github.com/go-chi/chi/v5"

	appcompute "github.com/JSYoo5B/sandstack/internal/app/compute"
)

const imageCatalogPath = "/image"

var errInvalidCreateImage = errors.New("Invalid input for createImage: expected a name and optional metadata.")

func (h handler) createServerImage(w http.ResponseWriter, r *http.Request, scope requestScope, raw json.RawMessage) {
	name, metadata, err := parseCreateImage(raw)
	if err != nil {
		writeFault(w, http.StatusBadRequest, "badRequest", err.Error())
		return
	}

	imageID, err := h.servers.CreateImage(scope.actionRequest(), chi.URLParam(r, "serverID"), name, metadata)
	var stateErr *appcompute.InvalidStateError
	switch {
	case errors.Is(err, appcompute.ErrServerNotFound):
		writeFault(w, http.StatusNotFound, "itemNotFound", "The server could not be found.")
	case errors.Is(err, appcompute.ErrImageMetadataQuota):
		writeFault(w, http.StatusForbidden, "forbidden", err.Error())
	case errors.As(err, &stateErr):
		writeFault(w, http.StatusConflict, "conflictingRequest", err.Error())
	case err != nil:
		writeFault(w, http.StatusInternalServerError, "computeFault", "Unable to create the server image.")
	default:
		w.Header().Set("Location", h.endpointURL(r, imageCatalogPath+"/images/"+url.PathEscape(imageID)))
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusAccepted)
	}
}

func parseCreateImage(raw json.RawMessage) (string, map[string]string, error) {
	fields, err := readUniqueObject(raw)
	if err != nil {
		return "", nil, errInvalidCreateImage
	}
	for key := range fields {
		if key != "name" && key != "metadata" {
			return "", nil, errors.New("Additional properties are not allowed ('" + key + "' was unexpected).")
		}
	}
	var name string
	if json.Unmarshal(fields["name"], &name) != nil || !validServerName(name) {
		return "", nil, errInvalidCreateImage
	}
	var metadata map[string]string
	if value, ok := fields["metadata"]; ok {
		if metadata, err = parseMetadataObject(value); err != nil {
			return "", nil, errInvalidCreateImage
		}
	}
	return name, metadata, nil
}
