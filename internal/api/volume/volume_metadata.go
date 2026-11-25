package volume

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	app "github.com/JSYoo5B/sandstack/internal/app/volume"
)

func (h handler) showVolumeMetadata(w http.ResponseWriter, r *http.Request, projectID string) {
	metadata, err := h.service.GetVolumeMetadata(projectID, chi.URLParam(r, "volumeID"))
	writeVolumeMetadata(w, metadata, err)
}

func changeVolumeMetadata(
	change func(string, string, map[string]string) (map[string]string, error),
) projectHandler {
	return func(w http.ResponseWriter, r *http.Request, projectID string) {
		data, ok := readJSONBody(w, r, "volume metadata")
		if !ok {
			return
		}
		values, err := parseMetadataRequest(data, cinderMetadataKey)
		if err != nil {
			writeFault(w, http.StatusBadRequest, "badRequest", err.Error())
			return
		}
		metadata, err := change(projectID, chi.URLParam(r, "volumeID"), values)
		writeVolumeMetadata(w, metadata, err)
	}
}

func writeVolumeMetadata(w http.ResponseWriter, values map[string]string, err error) {
	switch {
	case errors.Is(err, app.ErrVolumeNotFound):
		writeFault(w, http.StatusNotFound, "itemNotFound", "The volume could not be found.")
	case err != nil:
		writeFault(w, http.StatusInternalServerError, "computeFault", "Unable to access volume metadata.")
	default:
		writeJSON(w, http.StatusOK, metadataResponse{Metadata: values})
	}
}
