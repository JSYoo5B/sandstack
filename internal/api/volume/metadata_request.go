package volume

import (
	"encoding/json"
	"errors"
	"regexp"
	"unicode/utf8"
)

var errInvalidMetadataRequest = errors.New("expected a metadata object with valid keys and string values")
var cinderMetadataKey = regexp.MustCompile(`^[a-zA-Z0-9_.: /-]{1,255}$`)

type metadataResponse struct {
	Metadata map[string]string `json:"metadata"`
}

func parseMetadataRequest(data []byte, keyPattern *regexp.Regexp) (map[string]string, error) {
	if !utf8.Valid(data) {
		return nil, errInvalidMetadataRequest
	}
	envelope, err := readUniqueObject(data)
	if err != nil || len(envelope) != 1 {
		return nil, errInvalidMetadataRequest
	}
	return parseMetadataValues(envelope["metadata"], keyPattern)
}

func parseMetadataValues(data json.RawMessage, keyPattern *regexp.Regexp) (map[string]string, error) {
	fields, err := readUniqueObject(data)
	if err != nil {
		return nil, errInvalidMetadataRequest
	}
	metadata := make(map[string]string, len(fields))
	for key, raw := range fields {
		var value *string
		if !keyPattern.MatchString(key) || json.Unmarshal(raw, &value) != nil ||
			value == nil || utf8.RuneCountInString(*value) > maxVolumeTextLength {
			return nil, errInvalidMetadataRequest
		}
		metadata[key] = *value
	}
	return metadata, nil
}
