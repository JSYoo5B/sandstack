package compute

import (
	"encoding/json"
	"errors"
	"net/http"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	appcompute "github.com/JSYoo5B/sandstack/internal/app/compute"
)

const actionFault = "Expected a single os-start, os-stop, reboot, pause, unpause, suspend, resume, lock, or unlock action."

func (h handler) changeServerPower(w http.ResponseWriter, r *http.Request, scope requestScope) {
	data, ok := readRequestBody(w, r, "server action")
	if !ok {
		return
	}
	actions, err := readUniqueObject(data)
	if err != nil || !utf8.Valid(data) || len(actions) != 1 {
		writeFault(w, http.StatusBadRequest, "badRequest", actionFault)
		return
	}

	serverID := chi.URLParam(r, "serverID")
	switch {
	case actions["os-start"] != nil:
		err = h.service.SetPower(scope.projectID, serverID, true)
	case actions["os-stop"] != nil:
		err = h.service.SetPower(scope.projectID, serverID, false)
	case actions["reboot"] != nil:
		hard, valid := parseRebootType(actions["reboot"])
		if !valid {
			writeFault(w, http.StatusBadRequest, "badRequest", "Expected a reboot with type HARD or SOFT.")
			return
		}
		err = h.service.Reboot(scope.projectID, serverID, hard)
	case actions["pause"] != nil:
		err = h.service.Pause(scope.projectID, serverID)
	case actions["unpause"] != nil:
		err = h.service.Unpause(scope.projectID, serverID)
	case actions["suspend"] != nil:
		err = h.service.Suspend(scope.projectID, serverID)
	case actions["resume"] != nil:
		err = h.service.Resume(scope.projectID, serverID)
	case actions["lock"] != nil:
		err = h.service.Lock(scope.projectID, serverID)
	case actions["unlock"] != nil:
		err = h.service.Unlock(scope.projectID, serverID)
	default:
		writeFault(w, http.StatusBadRequest, "badRequest", actionFault)
		return
	}
	switch {
	case errors.Is(err, appcompute.ErrServerNotFound):
		writeFault(w, http.StatusNotFound, "itemNotFound", "The server could not be found.")
	case errors.Is(err, appcompute.ErrServerPowerState), errors.Is(err, appcompute.ErrServerLocked):
		writeFault(w, http.StatusConflict, "conflictingRequest", err.Error())
	case err != nil:
		writeFault(w, http.StatusInternalServerError, "computeFault", "Unable to change the server's power state.")
	default:
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusAccepted)
	}
}

func parseRebootType(data json.RawMessage) (hard, valid bool) {
	fields, err := readUniqueObject(data)
	if err != nil || len(fields) != 1 {
		return false, false
	}
	var rebootType string
	if err := json.Unmarshal(fields["type"], &rebootType); err != nil {
		return false, false
	}
	switch rebootType {
	case "HARD", "Hard", "hard":
		return true, true
	case "SOFT", "Soft", "soft":
		return false, true
	default:
		return false, false
	}
}
