package network

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const maxResourceNameLength = 255

var errInvalidCreateRequest = errors.New("expected one network object with valid fields")
var errInvalidObject = errors.New("expected one JSON object without repeated fields")
var resourceUUIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}(-[0-9a-fA-F]{4}){3}-[0-9a-fA-F]{12}$`)

type requestOwnership struct {
	projectID *string
	tenantID  *string
}

func (o requestOwnership) matches(projectID string) bool {
	return (o.projectID == nil || *o.projectID == projectID) &&
		(o.tenantID == nil || *o.tenantID == projectID)
}

type createNetworkParams struct {
	name         string
	adminStateUp bool
	shared       bool
	requestOwnership
}

func parseCreateNetwork(data []byte) (createNetworkParams, error) {
	params := createNetworkParams{adminStateUp: true}
	if len(bytes.Trim(data, " \t\r\n")) == 0 {
		return params, nil
	}
	fields, err := readCreateFields(data, "network")
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
		if !decodeCreateField(value, target) {
			return createNetworkParams{}, errInvalidCreateRequest
		}
	}
	if !validResourceName(params.name) {
		return createNetworkParams{}, errors.New("network name must be printable and contain at most 255 characters")
	}
	return params, nil
}

func validResourceName(name string) bool {
	return utf8.RuneCountInString(name) <= maxResourceNameLength && strings.TrimSpace(name) == name &&
		!strings.ContainsFunc(name, func(r rune) bool { return !unicode.IsGraphic(r) })
}

func decodeCreateField(value json.RawMessage, target any) bool {
	return !bytes.Equal(value, []byte("null")) && json.Unmarshal(value, target) == nil
}

func readCreateFields(data []byte, resource string) (map[string]json.RawMessage, error) {
	if !utf8.Valid(data) {
		return nil, errInvalidObject
	}
	envelope, err := readUniqueObject(data)
	if err != nil || len(envelope) != 1 {
		return nil, errInvalidObject
	}
	return readUniqueObject(envelope[resource])
}

// A regular JSON struct decode accepts repeated keys, allowing a later value
// to hide a different ownership or configuration value in the same request.
func readUniqueObject(data []byte) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return nil, errInvalidObject
	}
	fields := make(map[string]json.RawMessage)
	for decoder.More() {
		keyToken, err := decoder.Token()
		key, ok := keyToken.(string)
		if err != nil || !ok {
			return nil, errInvalidObject
		}
		if _, exists := fields[key]; exists {
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
