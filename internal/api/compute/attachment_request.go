package compute

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"
	"unicode/utf8"
)

var errAttachmentRequest = errors.New("expected one volumeAttachment object with a volume UUID and optional device")
var attachmentVolumeUUID = regexp.MustCompile(`^[0-9a-fA-F]{8}(-[0-9a-fA-F]{4}){3}-[0-9a-fA-F]{12}$`)

type attachmentRequest struct {
	volumeID string
	device   string
}

func parseAttachmentRequest(data []byte) (attachmentRequest, error) {
	if !utf8.Valid(data) {
		return attachmentRequest{}, errAttachmentRequest
	}
	envelope, err := readAttachmentObject(data)
	if err != nil || len(envelope) != 1 {
		return attachmentRequest{}, errAttachmentRequest
	}
	fields, err := readAttachmentObject(envelope["volumeAttachment"])
	if err != nil {
		return attachmentRequest{}, errAttachmentRequest
	}
	var request attachmentRequest
	for field, value := range fields {
		var target *string
		switch field {
		case "volumeId":
			target = &request.volumeID
		case "device":
			target = &request.device
		default:
			return attachmentRequest{}, errAttachmentRequest
		}
		if err := json.Unmarshal(value, target); err != nil {
			return attachmentRequest{}, errAttachmentRequest
		}
	}
	if !attachmentVolumeUUID.MatchString(request.volumeID) {
		return attachmentRequest{}, errAttachmentRequest
	}
	request.volumeID = strings.ToLower(request.volumeID)
	return request, nil
}

// Check both object levels for repeated JSON fields.
func readAttachmentObject(data []byte) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return nil, errAttachmentRequest
	}
	fields := make(map[string]json.RawMessage)
	for decoder.More() {
		token, err := decoder.Token()
		key, ok := token.(string)
		if err != nil || !ok || fields[key] != nil {
			return nil, errAttachmentRequest
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, errAttachmentRequest
		}
		fields[key] = value
	}
	end, err := decoder.Token()
	if err != nil || end != json.Delim('}') {
		return nil, errAttachmentRequest
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, errAttachmentRequest
	}
	return fields, nil
}
