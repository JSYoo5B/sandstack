package volume

import (
	"errors"
	"net/http"
	"regexp"

	"github.com/go-chi/chi/v5"

	app "github.com/JSYoo5B/sandstack/internal/app/volume"
)

var snapshotMetadataKey = regexp.MustCompile(`^[a-zA-Z0-9_.: -]{1,255}$`)

func (h handler) showSnapshotMetadata(w http.ResponseWriter, r *http.Request, projectID string) {
	snapshot, err := h.service.GetSnapshot(projectID, chi.URLParam(r, "snapshotID"))
	writeSnapshotMetadata(w, snapshot.Metadata, err)
}

func changeSnapshotMetadata(
	change func(string, string, map[string]string) (map[string]string, error),
) projectHandler {
	return func(w http.ResponseWriter, r *http.Request, projectID string) {
		data, ok := readJSONBody(w, r, "snapshot metadata")
		if !ok {
			return
		}
		values, err := parseMetadataRequest(data, snapshotMetadataKey)
		if err != nil {
			writeFault(w, http.StatusBadRequest, "badRequest", err.Error())
			return
		}
		metadata, err := change(projectID, chi.URLParam(r, "snapshotID"), values)
		writeSnapshotMetadata(w, metadata, err)
	}
}

func writeSnapshotMetadata(w http.ResponseWriter, values map[string]string, err error) {
	switch {
	case errors.Is(err, app.ErrSnapshotNotFound):
		writeFault(w, http.StatusNotFound, "itemNotFound", "The snapshot could not be found.")
	case err != nil:
		writeFault(w, http.StatusInternalServerError, "computeFault", "Unable to access snapshot metadata.")
	default:
		writeJSON(w, http.StatusOK, metadataResponse{Metadata: values})
	}
}
