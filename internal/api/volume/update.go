package volume

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	app "github.com/JSYoo5B/sandstack/internal/app/volume"
	domain "github.com/JSYoo5B/sandstack/internal/domain/volume"
)

func (h handler) updateVolume(w http.ResponseWriter, r *http.Request, projectID string) {
	data, ok := readJSONBody(w, r, "volume")
	if !ok {
		return
	}
	changes, err := parseUpdateVolume(data)
	if err != nil {
		writeFault(w, http.StatusBadRequest, "badRequest", err.Error())
		return
	}

	volume, err := h.service.Update(projectID, chi.URLParam(r, "volumeID"), changes)
	switch {
	case errors.Is(err, app.ErrVolumeNotFound):
		writeFault(w, http.StatusNotFound, "itemNotFound", "The volume could not be found.")
	case err != nil:
		writeFault(w, http.StatusInternalServerError, "computeFault", "Unable to update the volume.")
	default:
		writeJSON(w, http.StatusOK, volumeResponse{Volume: h.describeVolume(r, volume)})
	}
}

func parseUpdateVolume(data []byte) (app.TextUpdate, error) {
	if !utf8.Valid(data) {
		return app.TextUpdate{}, errInvalidVolumeObject
	}
	envelope, err := readUniqueObject(data)
	if err != nil || len(envelope) != 1 {
		return app.TextUpdate{}, errInvalidVolumeObject
	}
	fields, err := readUniqueObject(envelope["volume"])
	if err != nil {
		return app.TextUpdate{}, errInvalidVolumeObject
	}
	var changes app.TextUpdate
	for field, value := range fields {
		text, err := decodeNullableText(value)
		if err != nil {
			return app.TextUpdate{}, errInvalidVolumeObject
		}
		text.Value = strings.TrimSpace(text.Value)
		if utf8.RuneCountInString(text.Value) > maxVolumeTextLength {
			return app.TextUpdate{}, errors.New("volume name and description may contain up to 255 characters")
		}
		switch field {
		case "name":
			changes.Name = text
		case "description":
			changes.Description = text
		default:
			return app.TextUpdate{}, errInvalidVolumeObject
		}
	}
	return changes, nil
}

func decodeNullableText(data json.RawMessage) (*domain.NullableText, error) {
	var value *string
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, err
	}
	if value == nil {
		return &domain.NullableText{}, nil
	}
	return &domain.NullableText{Value: *value, Valid: true}, nil
}
