package volume

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"

	app "github.com/JSYoo5B/sandstack/internal/app/volume"
)

var errInvalidExtendAction = errors.New("expected one os-extend action with an integer new_size")

func parseExtendVolume(data []byte) (int, error) {
	fields, err := readUniqueObject(data)
	if err != nil || len(fields) != 1 {
		return 0, errInvalidExtendAction
	}
	var newSize *int64
	if err := json.Unmarshal(fields["new_size"], &newSize); err != nil || newSize == nil {
		return 0, errInvalidExtendAction
	}
	if *newSize < 1 || *newSize > math.MaxInt32 {
		return 0, errors.New("new_size must be between 1 and 2147483647 GiB")
	}
	return int(*newSize), nil
}

func (h handler) extendVolume(w http.ResponseWriter, projectID, volumeID string, data json.RawMessage) {
	newSize, err := parseExtendVolume(data)
	if err != nil {
		writeFault(w, http.StatusBadRequest, "badRequest", err.Error())
		return
	}
	err = h.service.StartExtension(projectID, volumeID, newSize)
	switch {
	case errors.Is(err, app.ErrVolumeNotFound):
		writeFault(w, http.StatusNotFound, "itemNotFound", "The volume could not be found.")
	case errors.Is(err, app.ErrInvalidExtensionSize):
		writeFault(w, http.StatusBadRequest, "badRequest", "The new size must be greater than the current volume size.")
	case errors.Is(err, app.ErrVolumeStateConflict):
		writeFault(w, http.StatusBadRequest, "badRequest", "Volume extension requires the available state.")
	case err != nil:
		writeFault(w, http.StatusInternalServerError, "computeFault", "Unable to extend the volume.")
	default:
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusAccepted)
	}
}
