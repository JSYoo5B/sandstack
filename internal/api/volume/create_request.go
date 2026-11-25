package volume

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"
	"unicode/utf8"
)

const maxVolumeTextLength = 255

var (
	errInvalidVolumeObject = errors.New("expected one volume object with valid fields")
	resourceUUID           = regexp.MustCompile(`^[0-9a-fA-F]{8}(-[0-9a-fA-F]{4}){3}-[0-9a-fA-F]{12}$`)
)

type createVolumeParams struct {
	name        string
	description string
	snapshotID  string
	volumeType  string
	size        int
	metadata    map[string]string
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
	var requestedZone *string
	var requestedSize *int
	for field, value := range fields {
		var target any
		if bytes.Equal(value, []byte("null")) {
			if field == "name" || field == "description" || field == "size" ||
				field == "metadata" || field == "volume_type" {
				continue
			}
			return createVolumeParams{}, errInvalidVolumeObject
		}
		switch field {
		case "size":
			target = &requestedSize
		case "snapshot_id":
			target = &params.snapshotID
		case "name":
			target = &params.name
		case "description":
			target = &params.description
		case "volume_type":
			target = &params.volumeType
		case "availability_zone":
			target = &requestedZone
		case "metadata":
			params.metadata, err = parseMetadataValues(value, cinderMetadataKey)
			if err != nil {
				return createVolumeParams{}, err
			}
			continue
		default:
			return createVolumeParams{}, errInvalidVolumeObject
		}
		if err := json.Unmarshal(value, target); err != nil {
			return createVolumeParams{}, errInvalidVolumeObject
		}
	}
	if fields["snapshot_id"] != nil && !resourceUUID.MatchString(params.snapshotID) {
		return createVolumeParams{}, errors.New("volume snapshot_id must be a UUID")
	}
	if requestedSize != nil {
		if params.snapshotID != "" && *requestedSize <= 0 {
			return createVolumeParams{}, errors.New("volume size must be positive")
		}
		params.size = *requestedSize
	} else if params.snapshotID == "" {
		if fields["size"] != nil {
			return createVolumeParams{}, errInvalidVolumeObject
		}
		return createVolumeParams{}, errors.New("volume size is required")
	}
	if utf8.RuneCountInString(params.volumeType) > maxVolumeTextLength {
		return createVolumeParams{}, errors.New("volume type may contain up to 255 characters")
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
