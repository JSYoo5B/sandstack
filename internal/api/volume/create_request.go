package volume

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"unicode/utf8"
)

const maxVolumeTextLength = 255

var errInvalidVolumeObject = errors.New("expected one volume object with valid fields")

type createVolumeParams struct {
	name        string
	description string
	size        int
}

func parseCreateVolume(data []byte) (createVolumeParams, error) {
	if !utf8.Valid(data) {
		return createVolumeParams{}, errInvalidVolumeObject
	}
	envelope, err := readUniqueObject(data)
	if err != nil || len(envelope) != 1 {
		return createVolumeParams{}, errInvalidVolumeObject
	}
	fields, err := readUniqueObject(envelope["volume"])
	if err != nil {
		return createVolumeParams{}, errInvalidVolumeObject
	}

	var params createVolumeParams
	var requestedType, requestedZone *string
	for field, value := range fields {
		var target any
		if bytes.Equal(value, []byte("null")) {
			if field == "name" || field == "description" {
				continue
			}
			return createVolumeParams{}, errInvalidVolumeObject
		}
		switch field {
		case "size":
			target = &params.size
		case "name":
			target = &params.name
		case "description":
			target = &params.description
		case "volume_type":
			target = &requestedType
		case "availability_zone":
			target = &requestedZone
		default:
			return createVolumeParams{}, errInvalidVolumeObject
		}
		if err := json.Unmarshal(value, target); err != nil {
			return createVolumeParams{}, errInvalidVolumeObject
		}
	}
	if fields["size"] == nil {
		return createVolumeParams{}, errors.New("volume size is required")
	}
	if requestedType != nil && *requestedType != volumeType {
		return createVolumeParams{}, errors.New("this endpoint supports the standard volume type")
	}
	if requestedZone != nil && *requestedZone != volumeZone {
		return createVolumeParams{}, errors.New("this endpoint supports the nova availability zone")
	}
	params.name = strings.TrimSpace(params.name)
	params.description = strings.TrimSpace(params.description)
	if utf8.RuneCountInString(params.name) > maxVolumeTextLength ||
		utf8.RuneCountInString(params.description) > maxVolumeTextLength {
		return createVolumeParams{}, errors.New("volume name and description may contain up to 255 characters")
	}
	return params, nil
}

// Check both object levels for repeated keys before accepting field values.
func readUniqueObject(data []byte) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return nil, errInvalidVolumeObject
	}
	fields := make(map[string]json.RawMessage)
	for decoder.More() {
		token, err := decoder.Token()
		key, ok := token.(string)
		if err != nil || !ok || fields[key] != nil {
			return nil, errInvalidVolumeObject
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, errInvalidVolumeObject
		}
		fields[key] = value
	}
	end, err := decoder.Token()
	if err != nil || end != json.Delim('}') {
		return nil, errInvalidVolumeObject
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, errInvalidVolumeObject
	}
	return fields, nil
}
