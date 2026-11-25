package volume

import (
	"errors"
	"io"
	"mime"
	"net/http"

	"github.com/go-chi/chi/v5"

	app "github.com/JSYoo5B/sandstack/internal/app/volume"
)

const maxCreateVolumeBody = 64 * 1024

func (h handler) createVolume(w http.ResponseWriter, r *http.Request, projectID string) {
	if contentType := r.Header.Get("Content-Type"); contentType != "" {
		mediaType, _, err := mime.ParseMediaType(contentType)
		if err != nil || mediaType != "application/json" {
			writeFault(w, http.StatusUnsupportedMediaType, "badMediaType", "Expected an application/json request.")
			return
		}
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxCreateVolumeBody))
	if err != nil {
		var sizeError *http.MaxBytesError
		if errors.As(err, &sizeError) {
			writeFault(w, http.StatusRequestEntityTooLarge, "overLimit", "The volume request is too large.")
			return
		}
		writeFault(w, http.StatusBadRequest, "badRequest", "Unable to read the volume request.")
		return
	}
	params, err := parseCreateVolume(data)
	if err != nil {
		writeFault(w, http.StatusBadRequest, "badRequest", err.Error())
		return
	}

	volume, err := h.service.Create(projectID, params.name, params.description, params.size)
	switch {
	case errors.Is(err, app.ErrVolumeNotFound):
		writeFault(w, http.StatusNotFound, "itemNotFound", "The volume project could not be found.")
	case errors.Is(err, app.ErrInvalidVolumeSize):
		writeFault(w, http.StatusBadRequest, "badRequest", err.Error())
	case err != nil:
		writeFault(w, http.StatusInternalServerError, "computeFault", "Unable to create the volume.")
	default:
		writeJSON(w, http.StatusAccepted, volumeResponse{Volume: h.describeVolume(r, volume)})
	}
}

func (h handler) deleteVolume(w http.ResponseWriter, r *http.Request, projectID string) {
	var firstByte [1]byte
	if _, err := io.ReadFull(r.Body, firstByte[:]); err != io.EOF {
		writeFault(w, http.StatusBadRequest, "badRequest", "Volume deletion requires an empty request body.")
		return
	}

	err := h.service.Delete(projectID, chi.URLParam(r, "volumeID"))
	switch {
	case errors.Is(err, app.ErrVolumeNotFound):
		writeFault(w, http.StatusNotFound, "itemNotFound", "The volume could not be found.")
	case errors.Is(err, app.ErrVolumeStateConflict):
		writeFault(w, http.StatusBadRequest, "badRequest", "Volume deletion requires an available or error state.")
	case err != nil:
		writeFault(w, http.StatusInternalServerError, "computeFault", "Unable to delete the volume.")
	default:
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusAccepted)
	}
}
