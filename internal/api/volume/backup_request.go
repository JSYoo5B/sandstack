package volume

import (
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"unicode/utf8"

	app "github.com/JSYoo5B/sandstack/internal/app/volume"
)

var (
	errInvalidBackupObject  = errors.New("expected one backup object with valid fields")
	errInvalidRestoreObject = errors.New("expected one restore object with an optional name and volume_id")
)

type createBackupParams struct {
	volumeID string
	options  app.BackupCreateOptions
}

func parseCreateBackup(data []byte) (createBackupParams, error) {
	fields, err := readEnvelope(data, "backup")
	if err != nil {
		return createBackupParams{}, errInvalidBackupObject
	}
	var params createBackupParams
	for field, value := range fields {
		switch field {
		case "volume_id":
			if json.Unmarshal(value, &params.volumeID) != nil || !resourceUUID.MatchString(params.volumeID) {
				return createBackupParams{}, errors.New("backup volume_id must be a UUID")
			}
		case "snapshot_id":
			var snapshotID *string
			if json.Unmarshal(value, &snapshotID) != nil ||
				(snapshotID != nil && !resourceUUID.MatchString(*snapshotID)) {
				return createBackupParams{}, errors.New("backup snapshot_id must be a UUID or null")
			}
			if snapshotID != nil {
				params.options.SnapshotID = *snapshotID
			}
		case "name", "description", "container":
			text, ok := parseOptionalText(value)
			if !ok {
				return createBackupParams{}, errors.New("backup " + field + " must be a string of up to 255 characters or null")
			}
			switch field {
			case "name":
				params.options.Name = trimText(text)
			case "description":
				params.options.Description = trimText(text)
			default:
				params.options.Container = text
			}
		case "incremental", "force":
			flag, ok := parseCinderBoolean(value)
			if !ok {
				return createBackupParams{}, errors.New("backup " + field + " must be a boolean")
			}
			if field == "incremental" {
				params.options.Incremental = flag
			} else {
				params.options.Force = flag
			}
		default:
			return createBackupParams{}, errInvalidBackupObject
		}
	}
	if params.volumeID == "" {
		return createBackupParams{}, errors.New("backup volume_id must be a UUID")
	}
	return params, nil
}

type restoreParams struct {
	volumeID string
	name     *string
}

func parseRestore(data []byte) (restoreParams, error) {
	fields, err := readEnvelope(data, "restore")
	if err != nil {
		return restoreParams{}, errInvalidRestoreObject
	}
	var params restoreParams
	for field, value := range fields {
		switch field {
		case "volume_id":
			var volumeID *string
			if json.Unmarshal(value, &volumeID) != nil || (volumeID != nil && !resourceUUID.MatchString(*volumeID)) {
				return restoreParams{}, errors.New("restore volume_id must be a UUID or null")
			}
			if volumeID != nil {
				params.volumeID = *volumeID
			}
		case "name":
			text, ok := parseOptionalText(value)
			if !ok {
				return restoreParams{}, errors.New("restore name must be a string of up to 255 characters or null")
			}
			params.name = text
		default:
			return restoreParams{}, errInvalidRestoreObject
		}
	}
	return params, nil
}

func readEnvelope(data []byte, key string) (map[string]json.RawMessage, error) {
	if !utf8.Valid(data) {
		return nil, errInvalidBackupObject
	}
	envelope, err := readUniqueObject(data)
	if err != nil || len(envelope) != 1 {
		return nil, errInvalidBackupObject
	}
	return readUniqueObject(envelope[key])
}

func parseOptionalText(value json.RawMessage) (*string, bool) {
	var text *string
	if json.Unmarshal(value, &text) != nil || (text != nil && utf8.RuneCountInString(*text) > maxVolumeTextLength) {
		return nil, false
	}
	return text, true
}

func trimText(text *string) *string {
	if text == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*text)
	return &trimmed
}

func parseBackupListQuery(query url.Values) (detailListOptions, *string, error) {
	var volumeID *string
	rest := make(url.Values, len(query))
	for field, values := range query {
		if field == "volume_id" {
			if len(values) != 1 {
				return detailListOptions{}, nil, errors.New("backup list parameters accept one value each")
			}
			volumeID = &values[0]
			continue
		}
		rest[field] = values
	}
	options, err := parseDetailListQuery(rest, "backup")
	return options, volumeID, err
}
