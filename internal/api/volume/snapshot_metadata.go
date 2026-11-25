package volume

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	app "github.com/JSYoo5B/sandstack/internal/app/volume"
)

var errSnapshotMetadataRequest = errors.New("expected a metadata object with valid keys and string values")
var snapshotMetadataKey = regexp.MustCompile(`^[a-zA-Z0-9_.: -]{1,255}$`)

type snapshotMetadataResponse struct {
	Metadata map[string]string `json:"metadata"`
}

func (h handler) showSnapshotMetadata(w http.ResponseWriter, r *http.Request, projectID string) {
	snapshot, err := h.service.GetSnapshot(projectID, chi.URLParam(r, "snapshotID"))
	writeSnapshotMetadata(w, snapshot.Metadata, err)
}

func (h handler) replaceSnapshotMetadata(w http.ResponseWriter, r *http.Request, projectID string) {
	data, ok := readJSONBody(w, r, "snapshot metadata")
	if !ok {
		return
	}
	values, err := parseSnapshotMetadata(data)
	if err != nil {
		writeFault(w, http.StatusBadRequest, "badRequest", err.Error())
		return
	}
	metadata, err := h.service.ReplaceSnapshotMetadata(projectID, chi.URLParam(r, "snapshotID"), values)
	writeSnapshotMetadata(w, metadata, err)
}

func writeSnapshotMetadata(w http.ResponseWriter, values map[string]string, err error) {
	switch {
	case errors.Is(err, app.ErrSnapshotNotFound):
		writeFault(w, http.StatusNotFound, "itemNotFound", "The snapshot could not be found.")
	case err != nil:
		writeFault(w, http.StatusInternalServerError, "computeFault", "Unable to access snapshot metadata.")
	default:
		writeJSON(w, http.StatusOK, snapshotMetadataResponse{Metadata: values})
	}
}

func parseSnapshotMetadata(data []byte) (map[string]string, error) {
	if !utf8.Valid(data) {
		return nil, errSnapshotMetadataRequest
	}
	envelope, err := readUniqueObject(data)
	if err != nil || len(envelope) != 1 {
		return nil, errSnapshotMetadataRequest
	}
	fields, err := readUniqueObject(envelope["metadata"])
	if err != nil {
		return nil, errSnapshotMetadataRequest
	}
	metadata := make(map[string]string, len(fields))
	for key, raw := range fields {
		var value *string
		if !snapshotMetadataKey.MatchString(key) || json.Unmarshal(raw, &value) != nil ||
			value == nil || utf8.RuneCountInString(*value) > maxVolumeTextLength {
			return nil, errSnapshotMetadataRequest
		}
		metadata[key] = *value
	}
	return metadata, nil
}
