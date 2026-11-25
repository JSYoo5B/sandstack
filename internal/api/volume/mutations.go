package volume

import (
	"errors"
	"io"
	"mime"
	"net/http"

	"github.com/go-chi/chi/v5"

	app "github.com/JSYoo5B/sandstack/internal/app/volume"
	domain "github.com/JSYoo5B/sandstack/internal/domain/volume"
)

const maxJSONBody = 64 * 1024

func readJSONBody(w http.ResponseWriter, r *http.Request, resource string) ([]byte, bool) {
	if contentType := r.Header.Get("Content-Type"); contentType != "" {
		mediaType, _, err := mime.ParseMediaType(contentType)
		if err != nil || mediaType != "application/json" {
			writeFault(w, http.StatusUnsupportedMediaType, "badMediaType", "Expected an application/json request.")
			return nil, false
		}
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxJSONBody))
	if err != nil {
		var sizeError *http.MaxBytesError
		if errors.As(err, &sizeError) {
			writeFault(w, http.StatusRequestEntityTooLarge, "overLimit", "The "+resource+" request is too large.")
			return nil, false
		}
		writeFault(w, http.StatusBadRequest, "badRequest", "Unable to read the "+resource+" request.")
		return nil, false
	}
	return data, true
}

func (h handler) createVolume(w http.ResponseWriter, r *http.Request, projectID string) {
	data, ok := readJSONBody(w, r, "volume")
	if !ok {
		return
	}
	params, err := parseCreateVolume(data)
	if err != nil {
		writeFault(w, http.StatusBadRequest, "badRequest", err.Error())
		return
	}
	if err := h.service.ValidateCreateVolumeType(projectID, params.volumeType); err != nil {
		writeFault(w, http.StatusNotFound, "itemNotFound", "The volume type could not be found.")
		return
	}

	var volume domain.Volume
	if params.snapshotID == "" {
		volume, err = h.service.CreateWithMetadata(
			projectID, params.name, params.description, params.size, params.metadata,
		)
	} else {
		volume, err = h.service.CreateFromSnapshotWithMetadata(
			projectID, params.snapshotID, params.name, params.description, params.size, params.metadata,
		)
	}
	switch {
	case errors.Is(err, app.ErrVolumeNotFound):
		writeFault(w, http.StatusNotFound, "itemNotFound", "The volume project could not be found.")
	case errors.Is(err, app.ErrSnapshotNotFound):
		writeFault(w, http.StatusNotFound, "itemNotFound", "The source snapshot could not be found.")
	case errors.Is(err, app.ErrSnapshotStateConflict):
		writeFault(w, http.StatusBadRequest, "badRequest", "The source snapshot must be available.")
	case errors.Is(err, app.ErrInvalidVolumeSize), errors.Is(err, app.ErrVolumeSmallerThanSnapshot):
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
	case errors.Is(err, app.ErrVolumeHasSnapshots):
		writeFault(w, http.StatusBadRequest, "badRequest", "The volume has snapshots.")
	case errors.Is(err, app.ErrVolumeStateConflict):
		writeFault(w, http.StatusBadRequest, "badRequest", "Volume deletion requires the available, error, or error_extending state.")
	case err != nil:
		writeFault(w, http.StatusInternalServerError, "computeFault", "Unable to delete the volume.")
	default:
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusAccepted)
	}
}
