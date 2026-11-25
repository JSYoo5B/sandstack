package compute

import (
	"errors"
	"net/http"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	appcompute "github.com/JSYoo5B/sandstack/internal/app/compute"
)

func (h handler) changeServerPower(w http.ResponseWriter, r *http.Request, scope requestScope) {
	data, ok := readRequestBody(w, r, "server action")
	if !ok {
		return
	}
	actions, err := readUniqueObject(data)
	_, start := actions["os-start"]
	_, stop := actions["os-stop"]
	if err != nil || !utf8.Valid(data) || len(actions) != 1 || (!start && !stop) {
		writeFault(w, http.StatusBadRequest, "badRequest", "Expected a single os-start or os-stop action.")
		return
	}

	err = h.service.SetPower(scope.projectID, chi.URLParam(r, "serverID"), start)
	switch {
	case errors.Is(err, appcompute.ErrServerNotFound):
		writeFault(w, http.StatusNotFound, "itemNotFound", "The server could not be found.")
	case errors.Is(err, appcompute.ErrServerPowerState):
		writeFault(w, http.StatusConflict, "conflictingRequest", err.Error())
	case err != nil:
		writeFault(w, http.StatusInternalServerError, "computeFault", "Unable to change the server's power state.")
	default:
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusAccepted)
	}
}
