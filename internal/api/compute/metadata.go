package compute

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	appcompute "github.com/JSYoo5B/sandstack/internal/app/compute"
)

var metadataKey = regexp.MustCompile(`^[a-zA-Z0-9_.: -]{1,255}$`)
var errInvalidMetadata = errors.New("expected a metadata object with valid keys and string values")

type metadataResponse struct {
	Metadata map[string]string `json:"metadata"`
}

func (h handler) showServerMetadata(w http.ResponseWriter, r *http.Request, scope requestScope) {
	values, err := h.service.GetMetadata(scope.projectID, chi.URLParam(r, "serverID"))
	writeMetadataResult(w, http.StatusOK, metadataResponse{Metadata: values}, err)
}

func changeServerMetadata(
	change func(string, string, map[string]string) (map[string]string, error),
) func(http.ResponseWriter, *http.Request, requestScope) {
	return func(w http.ResponseWriter, r *http.Request, scope requestScope) {
		data, ok := readRequestBody(w, r, "server metadata")
		if !ok {
			return
		}
		values, err := parseMetadata(data, "metadata")
		if err != nil {
			writeFault(w, http.StatusBadRequest, "badRequest", err.Error())
			return
		}
		saved, err := change(scope.projectID, chi.URLParam(r, "serverID"), values)
		writeMetadataResult(w, http.StatusOK, metadataResponse{Metadata: saved}, err)
	}
}

func writeMetadataResult(w http.ResponseWriter, status int, body any, err error) {
	switch {
	case errors.Is(err, appcompute.ErrServerNotFound):
		writeFault(w, http.StatusNotFound, "itemNotFound", "The server could not be found.")
	case errors.Is(err, appcompute.ErrServerMetadatumNotFound):
		writeFault(w, http.StatusNotFound, "itemNotFound", "The metadata item could not be found.")
	case errors.Is(err, appcompute.ErrServerMetadataState), errors.Is(err, appcompute.ErrServerLocked):
		writeFault(w, http.StatusConflict, "conflictingRequest", err.Error())
	case errors.Is(err, appcompute.ErrServerMetadataQuota):
		writeFault(w, http.StatusForbidden, "forbidden", err.Error())
	case err != nil:
		writeFault(w, http.StatusInternalServerError, "computeFault", "Unable to access server metadata.")
	case status == http.StatusNoContent:
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(status)
	default:
		writeJSON(w, status, body)
	}
}

func parseMetadata(data []byte, wrapper string) (map[string]string, error) {
	envelope, err := readUniqueObject(data)
	if err != nil || !utf8.Valid(data) || len(envelope) != 1 {
		return nil, errInvalidMetadata
	}
	return parseMetadataObject(envelope[wrapper])
}

func parseMetadataObject(raw json.RawMessage) (map[string]string, error) {
	fields, err := readUniqueObject(raw)
	if err != nil {
		return nil, errInvalidMetadata
	}
	values := make(map[string]string, len(fields))
	for key, raw := range fields {
		var value *string
		if !metadataKey.MatchString(key) || json.Unmarshal(raw, &value) != nil ||
			value == nil || utf8.RuneCountInString(*value) > 255 {
			return nil, errInvalidMetadata
		}
		values[key] = *value
	}
	return values, nil
}
