package volume

import (
	"errors"
	"io"
	"mime"
	"net/http"

	"github.com/go-chi/chi/v5"

	app "github.com/JSYoo5B/sandstack/internal/app/volume"
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
	userID, ok := h.requestUser(w, r)
	if !ok {
		return
	}
	volume, err := h.service.CreateVolume(projectID, userID, app.CreateRequest{
		VolumeType:     params.volumeType,
		ImageID:        params.imageID,
		SnapshotID:     params.snapshotID,
		SourceVolumeID: params.sourceVolumeID,
		Name:           params.name,
		Description:    params.description,
		Size:           params.size,
		Metadata:       params.metadata,
	})
	switch {
	case errors.Is(err, app.ErrVolumeTypeNotFound):
		writeFault(w, http.StatusNotFound, "itemNotFound", "The volume type could not be found.")
	case errors.Is(err, app.ErrSnapshotNotFound):
		writeFault(w, http.StatusNotFound, "itemNotFound", "The source snapshot could not be found.")
	case errors.Is(err, app.ErrVolumeSourceNotFound):
		writeFault(w, http.StatusNotFound, "itemNotFound", "The source volume could not be found.")
	case errors.Is(err, app.ErrSnapshotStateConflict):
		writeFault(w, http.StatusBadRequest, "badRequest", "The source snapshot must be available.")
	case errors.Is(err, app.ErrVolumeSourceImageUnavailable), errors.Is(err, app.ErrVolumeSourceImageSize):
		writeFault(w, http.StatusBadRequest, "badRequest", err.Error())
	case errors.Is(err, app.ErrVolumeSourceState), errors.Is(err, app.ErrVolumeSmallerThanSource):
		writeFault(w, http.StatusBadRequest, "badRequest", err.Error())
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
		writeFault(w, http.StatusBadRequest, "badRequest", "Volume deletion requires the available, error, error_extending, or error_restoring state.")
	case err != nil:
		writeFault(w, http.StatusInternalServerError, "computeFault", "Unable to delete the volume.")
	default:
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusAccepted)
	}
}
