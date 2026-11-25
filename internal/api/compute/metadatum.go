package compute

import (
	"net/http"
	"net/url"

	"github.com/go-chi/chi/v5"
)

type metadatumResponse struct {
	Meta map[string]string `json:"meta"`
}

func (h handler) showServerMetadatum(w http.ResponseWriter, r *http.Request, scope requestScope) {
	key := metadataPathKey(r)
	value, err := h.service.GetMetadatum(scope.projectID, chi.URLParam(r, "serverID"), key)
	writeMetadataResult(w, http.StatusOK, metadatumResponse{Meta: map[string]string{key: value}}, err)
}

func (h handler) writeServerMetadatum(w http.ResponseWriter, r *http.Request, scope requestScope) {
	data, ok := readRequestBody(w, r, "server metadata item")
	if !ok {
		return
	}
	values, err := parseMetadata(data, "meta")
	if err != nil || len(values) != 1 {
		writeFault(w, http.StatusBadRequest, "badRequest", "Expected a meta object with one metadata key and value.")
		return
	}
	key := metadataPathKey(r)
	value, found := values[key]
	if !found {
		writeFault(w, http.StatusBadRequest, "badRequest", "The metadata key must match the URL.")
		return
	}
	err = h.service.PutMetadatum(scope.projectID, chi.URLParam(r, "serverID"), key, value)
	writeMetadataResult(w, http.StatusOK, metadatumResponse{Meta: values}, err)
}

func (h handler) deleteServerMetadatum(w http.ResponseWriter, r *http.Request, scope requestScope) {
	err := h.service.DeleteMetadatum(
		scope.projectID, chi.URLParam(r, "serverID"), metadataPathKey(r),
	)
	writeMetadataResult(w, http.StatusNoContent, nil, err)
}

func metadataPathKey(r *http.Request) string {
	key := chi.URLParam(r, "metadataKey")
	// Chi captures escaped parameters when it uses RawPath for routing.
	if r.URL.RawPath != "" {
		key, _ = url.PathUnescape(key)
	}
	return key
}
