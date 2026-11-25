package compute

import (
	"encoding/json"
	"errors"
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
	envelope, err := readUniqueObject(data)
	if err != nil || len(envelope) != 1 {
		return attachmentRequest{}, errAttachmentRequest
	}
	fields, err := readUniqueObject(envelope["volumeAttachment"])
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
