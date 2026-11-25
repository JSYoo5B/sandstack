package compute

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	appcompute "github.com/JSYoo5B/sandstack/internal/app/compute"
	domaincompute "github.com/JSYoo5B/sandstack/internal/domain/compute"
)

var errInvalidServerUpdate = errors.New("expected a server object containing only an optional valid name")

type updatedServerBody struct {
	serverCore
	AccessIPv4 string `json:"accessIPv4"`
	AccessIPv6 string `json:"accessIPv6"`
	HostID     string `json:"hostId"`
	DiskConfig string `json:"OS-DCF:diskConfig"`
	Progress   *int   `json:"progress,omitempty"`
}

func (h handler) updateServer(w http.ResponseWriter, r *http.Request, scope requestScope) {
	data, ok := readRequestBody(w, r, "server update")
	if !ok {
		return
	}
	name, err := parseServerNameUpdate(data)
	if err != nil {
		writeFault(w, http.StatusBadRequest, "badRequest", err.Error())
		return
	}
	server, err := h.service.UpdateName(scope.projectID, chi.URLParam(r, "serverID"), name)
	switch {
	case errors.Is(err, appcompute.ErrServerNotFound):
		writeFault(w, http.StatusNotFound, "itemNotFound", "The server could not be found.")
	case err != nil:
		writeFault(w, http.StatusInternalServerError, "computeFault", "Unable to update the server.")
	default:
		body := updatedServerBody{
			serverCore: h.serverCore(r, server, scope.pathProject),
			DiskConfig: "MANUAL",
		}
		if server.Status() == domaincompute.StatusActive {
			progress := 0
			body.Progress = &progress
		}
		writeJSON(w, http.StatusOK, struct {
			Server updatedServerBody `json:"server"`
		}{Server: body})
	}
}

func parseServerNameUpdate(data []byte) (*string, error) {
	envelope, err := readUniqueObject(data)
	if err != nil || !utf8.Valid(data) || len(envelope) != 1 {
		return nil, errInvalidServerUpdate
	}
	fields, err := readUniqueObject(envelope["server"])
	if err != nil {
		return nil, errInvalidServerUpdate
	}
	if len(fields) == 0 {
		return nil, nil
	}
	var name string
	if len(fields) != 1 || json.Unmarshal(fields["name"], &name) != nil ||
		name == "" || utf8.RuneCountInString(name) > 255 || strings.TrimSpace(name) != name {
		return nil, errInvalidServerUpdate
	}
	// Nova builds its printable name character ranges from code points below U+FFFF.
	for _, char := range name {
		if char >= 0xFFFF || !unicode.IsGraphic(char) {
			return nil, errInvalidServerUpdate
		}
	}
	return &name, nil
}
