package volume

import (
	"encoding/json"
	"errors"
	"net/http"

	app "github.com/JSYoo5B/sandstack/internal/app/volume"
)

var errInvalidRetypeAction = errors.New(
	"expected os-retype with a string new_type and an optional migration_policy of never or on-demand",
)

func parseRetype(data json.RawMessage) (string, error) {
	fields, err := readUniqueObject(data)
	if err != nil {
		return "", errInvalidRetypeAction
	}
	var newType *string
	if json.Unmarshal(fields["new_type"], &newType) != nil || newType == nil {
		return "", errInvalidRetypeAction
	}
	for field, raw := range fields {
		switch field {
		case "new_type":
		case "migration_policy":
			var policy *string
			if json.Unmarshal(raw, &policy) != nil || policy == nil ||
				(*policy != "never" && *policy != "on-demand") {
				return "", errInvalidRetypeAction
			}
		default:
			return "", errInvalidRetypeAction
		}
	}
	return *newType, nil
}

func (h handler) retypeVolume(w http.ResponseWriter, projectID, volumeID string, data json.RawMessage) {
	newType, err := parseRetype(data)
	if err != nil {
		writeFault(w, http.StatusBadRequest, "badRequest", err.Error())
		return
	}
	err = h.service.Retype(projectID, volumeID, newType)
	switch {
	case errors.Is(err, app.ErrVolumeNotFound):
		writeFault(w, http.StatusNotFound, "itemNotFound", "The volume could not be found.")
	case errors.Is(err, app.ErrVolumeTypeNotFound):
		writeFault(w, http.StatusNotFound, "itemNotFound", "The volume type could not be found.")
	case errors.Is(err, app.ErrVolumeStateConflict):
		writeFault(w, http.StatusBadRequest, "badRequest", "Retype requires the available or in-use state.")
	case errors.Is(err, app.ErrVolumeTypeUnchanged):
		writeFault(w, http.StatusBadRequest, "badRequest", "The requested type must differ from the volume type.")
	case err != nil:
		writeFault(w, http.StatusInternalServerError, "computeFault", "Unable to retype the volume.")
	default:
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusAccepted)
	}
}
