package volume

import (
	"errors"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	app "github.com/JSYoo5B/sandstack/internal/app/volume"
)

func (h handler) updateSnapshot(w http.ResponseWriter, r *http.Request, projectID string) {
	data, ok := readJSONBody(w, r, "snapshot")
	if !ok {
		return
	}
	changes, err := parseUpdateSnapshot(data)
	if err != nil {
		writeFault(w, http.StatusBadRequest, "badRequest", err.Error())
		return
	}
	snapshot, previousUpdatedAt, err := h.service.UpdateSnapshot(projectID, chi.URLParam(r, "snapshotID"), changes)
	switch {
	case errors.Is(err, app.ErrSnapshotNotFound):
		writeFault(w, http.StatusNotFound, "itemNotFound", "The snapshot could not be found.")
	case err != nil:
		writeFault(w, http.StatusInternalServerError, "computeFault", "Unable to update the snapshot.")
	default:
		// Cinder renders the update response without reloading the database timestamp.
		snapshot.UpdatedAt = previousUpdatedAt
		writeJSON(w, http.StatusOK, snapshotResponse{Snapshot: describeSnapshot(snapshot)})
	}
}

func parseUpdateSnapshot(data []byte) (app.TextUpdate, error) {
	if !utf8.Valid(data) {
		return app.TextUpdate{}, errInvalidSnapshotObject
	}
	envelope, err := readUniqueObject(data)
	if err != nil || len(envelope) != 1 {
		return app.TextUpdate{}, errInvalidSnapshotObject
	}
	fields, err := readUniqueObject(envelope["snapshot"])
	if err != nil || len(fields) == 0 {
		return app.TextUpdate{}, errInvalidSnapshotObject
	}
	var changes app.TextUpdate
	for field, value := range fields {
		text, err := decodeNullableText(value)
		if err != nil || utf8.RuneCountInString(text.Value) > maxVolumeTextLength {
			return app.TextUpdate{}, errInvalidSnapshotObject
		}
		switch field {
		case "name":
			changes.Name = text
		case "description":
			if strings.ContainsFunc(text.Value, func(r rune) bool { return !unicode.IsGraphic(r) }) {
				return app.TextUpdate{}, errInvalidSnapshotObject
			}
			changes.Description = text
		default:
			return app.TextUpdate{}, errInvalidSnapshotObject
		}
		text.Value = strings.TrimSpace(text.Value)
	}
	return changes, nil
}
