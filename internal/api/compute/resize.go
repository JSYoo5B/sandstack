package compute

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	appcompute "github.com/JSYoo5B/sandstack/internal/app/compute"
)

const diskConfigField = "OS-DCF:diskConfig"

var errInvalidResize = errors.New("Invalid input for resize: expected a flavorRef and optional OS-DCF:diskConfig.")

func (h handler) resizeServer(w http.ResponseWriter, r *http.Request, scope requestScope, raw json.RawMessage) {
	flavorID, autoDiskConfig, err := parseResize(raw)
	if err != nil {
		writeFault(w, http.StatusBadRequest, "badRequest", err.Error())
		return
	}

	err = h.servers.Resize(scope.actionRequest(), chi.URLParam(r, "serverID"), flavorID, autoDiskConfig)
	writeResizeResult(w, err, http.StatusAccepted)
}

func (h handler) finishServerResize(w http.ResponseWriter, r *http.Request, scope requestScope, revert bool) {
	serverID := chi.URLParam(r, "serverID")
	if revert {
		writeResizeResult(w, h.service.RevertResize(scope.actionRequest(), serverID), http.StatusAccepted)
		return
	}
	writeResizeResult(w, h.service.ConfirmResize(scope.actionRequest(), serverID), http.StatusNoContent)
}

func writeResizeResult(w http.ResponseWriter, err error, success int) {
	var stateErr *appcompute.InvalidStateError
	var lockedErr *appcompute.LockedError
	switch {
	case errors.Is(err, appcompute.ErrServerNotFound):
		writeFault(w, http.StatusNotFound, "itemNotFound", "The server could not be found.")
	case errors.Is(err, appcompute.ErrInvalidResize):
		writeFault(w, http.StatusBadRequest, "badRequest", err.Error())
	case errors.As(err, &lockedErr), errors.As(err, &stateErr):
		writeFault(w, http.StatusConflict, "conflictingRequest", err.Error())
	case err != nil:
		writeFault(w, http.StatusInternalServerError, "computeFault", "Unable to resize the server.")
	default:
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(success)
	}
}

func parseResize(raw json.RawMessage) (string, *bool, error) {
	fields, err := readUniqueObject(raw)
	if err != nil {
		return "", nil, errInvalidResize
	}
	for key := range fields {
		if key != "flavorRef" && key != diskConfigField {
			return "", nil, errors.New("Additional properties are not allowed ('" + key + "' was unexpected).")
		}
	}
	value, ok := fields["flavorRef"]
	if !ok {
		return "", nil, errors.New("'flavorRef' is a required property.")
	}
	flavorID, ok := parseResizeFlavorRef(value)
	if !ok {
		return "", nil, errors.New("Invalid input for field 'flavorRef'.")
	}
	value, ok = fields[diskConfigField]
	if !ok {
		return flavorID, nil, nil
	}
	var diskConfig string
	if json.Unmarshal(value, &diskConfig) != nil || (diskConfig != "AUTO" && diskConfig != "MANUAL") {
		return "", nil, errors.New("Invalid input for field '" + diskConfigField + "'.")
	}
	auto := diskConfig == "AUTO"
	return flavorID, &auto, nil
}

func parseResizeFlavorRef(raw json.RawMessage) (string, bool) {
	var ref string
	if json.Unmarshal(raw, &ref) == nil {
		return ref, ref != ""
	}
	var number json.Number
	if json.Unmarshal(raw, &number) != nil {
		return "", false
	}
	if _, err := strconv.ParseInt(number.String(), 10, 64); err != nil {
		return "", false
	}
	return number.String(), true
}
