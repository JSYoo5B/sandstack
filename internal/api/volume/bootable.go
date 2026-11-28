package volume

import (
	"encoding/json"
	"errors"
	"net/http"

	app "github.com/JSYoo5B/sandstack/internal/app/volume"
)

var errInvalidBootableAction = errors.New("expected a bootable boolean or a supported boolean string")

func (h handler) setVolumeBootable(w http.ResponseWriter, projectID, volumeID string, data json.RawMessage) {
	bootable, err := parseBootable(data)
	if err != nil {
		writeFault(w, http.StatusBadRequest, "badRequest", err.Error())
		return
	}
	err = h.service.SetBootable(projectID, volumeID, bootable)
	switch {
	case errors.Is(err, app.ErrVolumeNotFound):
		writeFault(w, http.StatusNotFound, "itemNotFound", "The volume could not be found.")
	case err != nil:
		writeFault(w, http.StatusInternalServerError, "computeFault", "Unable to update the volume bootable flag.")
	default:
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
	}
}

func parseBootable(data json.RawMessage) (bool, error) {
	fields, err := readUniqueObject(data)
	if err != nil || len(fields) != 1 {
		return false, errInvalidBootableAction
	}
	bootable, ok := parseCinderBoolean(fields["bootable"])
	if !ok {
		return false, errInvalidBootableAction
	}
	return bootable, nil
}

func parseCinderBoolean(data json.RawMessage) (bool, bool) {
	var value any
	if json.Unmarshal(data, &value) != nil {
		return false, false
	}
	switch typed := value.(type) {
	case bool:
		return typed, true
	case string:
		switch typed {
		case "True", "TRUE", "true", "1", "ON", "On", "on", "YES", "Yes", "yes", "y", "t":
			return true, true
		case "False", "FALSE", "false", "0", "OFF", "Off", "off", "NO", "No", "no", "n", "f":
			return false, true
		}
	}
	return false, false
}
