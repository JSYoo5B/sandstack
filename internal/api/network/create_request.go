package network

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"
)

const maxNetworkNameLength = 255

var errInvalidCreateRequest = errors.New("expected one network object with valid fields")

type createNetworkParams struct {
	name         string
	adminStateUp bool
	shared       bool
	projectID    *string
	tenantID     *string
}

func parseCreateNetwork(data []byte) (createNetworkParams, error) {
	params := createNetworkParams{adminStateUp: true}
	if len(bytes.Trim(data, " \t\r\n")) == 0 {
		return params, nil
	}
	if !utf8.Valid(data) {
		return createNetworkParams{}, errInvalidCreateRequest
	}
	envelope, err := readUniqueObject(data)
	if err != nil || len(envelope) != 1 {
		return createNetworkParams{}, errInvalidCreateRequest
	}
	fields, err := readUniqueObject(envelope["network"])
	if err != nil {
		return createNetworkParams{}, errInvalidCreateRequest
	}
	for field, value := range fields {
		var target any
		switch field {
		case "name":
			target = &params.name
		case "admin_state_up":
			target = &params.adminStateUp
		case "shared":
			target = &params.shared
		case "project_id":
			target = &params.projectID
		case "tenant_id":
			target = &params.tenantID
		default:
			return createNetworkParams{}, errInvalidCreateRequest
		}
		if bytes.Equal(value, []byte("null")) || json.Unmarshal(value, target) != nil {
			return createNetworkParams{}, errInvalidCreateRequest
		}
	}
	if utf8.RuneCountInString(params.name) > maxNetworkNameLength || strings.TrimSpace(params.name) != params.name ||
		strings.ContainsFunc(params.name, func(r rune) bool { return !unicode.IsGraphic(r) }) {
		return createNetworkParams{}, errors.New("network name must be printable and contain at most 255 characters")
	}
	return params, nil
}

// A regular JSON struct decode accepts repeated keys, allowing a later value
// to hide a different ownership or configuration value in the same request.
func readUniqueObject(data []byte) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return nil, errInvalidCreateRequest
	}
	fields := make(map[string]json.RawMessage)
	for decoder.More() {
		keyToken, err := decoder.Token()
		key, ok := keyToken.(string)
		if err != nil || !ok {
			return nil, errInvalidCreateRequest
		}
		if _, exists := fields[key]; exists {
			return nil, errInvalidCreateRequest
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, errInvalidCreateRequest
		}
		fields[key] = value
	}
	end, err := decoder.Token()
	if err != nil || end != json.Delim('}') {
		return nil, errInvalidCreateRequest
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, errInvalidCreateRequest
	}
	return fields, nil
}
