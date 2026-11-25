package volume

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"
)

var errInvalidSnapshotObject = errors.New("expected one snapshot object with valid fields")

type createSnapshotParams struct {
	volumeID    string
	name        string
	description string
	force       bool
	metadata    map[string]string
}

func parseCreateSnapshot(data []byte) (createSnapshotParams, error) {
	if !utf8.Valid(data) {
		return createSnapshotParams{}, errInvalidSnapshotObject
	}
	envelope, err := readUniqueObject(data)
	if err != nil || len(envelope) != 1 {
		return createSnapshotParams{}, errInvalidSnapshotObject
	}
	fields, err := readUniqueObject(envelope["snapshot"])
	if err != nil {
		return createSnapshotParams{}, errInvalidSnapshotObject
	}

	var params createSnapshotParams
	for field, value := range fields {
		var target *string
		switch field {
		case "volume_id":
			target = &params.volumeID
		case "name":
			target = &params.name
		case "description":
			target = &params.description
		case "force":
			var force *bool
			if json.Unmarshal(value, &force) != nil || force == nil {
				return createSnapshotParams{}, errors.New("snapshot force must be a boolean")
			}
			params.force = *force
			continue
		case "metadata":
			if bytes.Equal(value, []byte("null")) {
				continue
			}
			params.metadata, err = parseMetadataValues(value, cinderMetadataKey)
			if err != nil {
				return createSnapshotParams{}, err
			}
			continue
		default:
			return createSnapshotParams{}, errInvalidSnapshotObject
		}
		if err := json.Unmarshal(value, target); err != nil {
			return createSnapshotParams{}, errInvalidSnapshotObject
		}
	}
	if !resourceUUID.MatchString(params.volumeID) {
		return createSnapshotParams{}, errors.New("snapshot volume_id must be a UUID")
	}
	params.name = strings.TrimSpace(params.name)
	params.description = strings.TrimSpace(params.description)
	if utf8.RuneCountInString(params.name) > maxVolumeTextLength ||
		utf8.RuneCountInString(params.description) > maxVolumeTextLength {
		return createSnapshotParams{}, errors.New("snapshot name and description may contain up to 255 characters")
	}
	return params, nil
}
