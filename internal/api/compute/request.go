package compute

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
)

const maxRequestBody = 64 * 1024

var errInvalidObject = errors.New("expected a JSON object without repeated fields")

func readRequestBody(w http.ResponseWriter, r *http.Request, operation string) ([]byte, bool) {
	if contentType := r.Header.Get("Content-Type"); contentType != "" {
		mediaType, _, err := mime.ParseMediaType(contentType)
		if err != nil || mediaType != "application/json" {
			writeFault(w, http.StatusUnsupportedMediaType, "badMediaType", "Expected an application/json request.")
			return nil, false
		}
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRequestBody))
	if err != nil {
		var sizeError *http.MaxBytesError
		if errors.As(err, &sizeError) {
			writeFault(w, http.StatusRequestEntityTooLarge, "overLimit", "The "+operation+" request is too large.")
			return nil, false
		}
		writeFault(w, http.StatusBadRequest, "badRequest", "Unable to read the "+operation+" request.")
		return nil, false
	}
	return data, true
}

func readUniqueObject(data []byte) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return nil, errInvalidObject
	}
	fields := make(map[string]json.RawMessage)
	for decoder.More() {
		token, err := decoder.Token()
		key, ok := token.(string)
		if err != nil || !ok || fields[key] != nil {
			return nil, errInvalidObject
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, errInvalidObject
		}
		fields[key] = value
	}
	end, err := decoder.Token()
	if err != nil || end != json.Delim('}') {
		return nil, errInvalidObject
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, errInvalidObject
	}
	return fields, nil
}
