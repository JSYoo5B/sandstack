package compute

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"
)

var (
	errKeyPairRequest = errors.New("expected one keypair object with a valid name and an optional public_key string")
	keyPairName       = regexp.MustCompile(`^[-_ a-zA-Z0-9]{1,255}$`)
)

type keyPairRequest struct {
	name      string
	publicKey *string
}

func parseKeyPairRequest(data []byte) (keyPairRequest, error) {
	if !utf8.Valid(data) {
		return keyPairRequest{}, errKeyPairRequest
	}
	envelope, err := readUniqueObject(data)
	if err != nil || len(envelope) != 1 {
		return keyPairRequest{}, errKeyPairRequest
	}
	fields, err := readUniqueObject(envelope["keypair"])
	if err != nil {
		return keyPairRequest{}, errKeyPairRequest
	}
	var request keyPairRequest
	for field, value := range fields {
		text, ok := jsonString(value)
		if !ok {
			return keyPairRequest{}, errKeyPairRequest
		}
		switch field {
		case "name":
			request.name = text
		case "public_key":
			request.publicKey = &text
		default:
			return keyPairRequest{}, errKeyPairRequest
		}
	}
	if !keyPairName.MatchString(request.name) || strings.TrimSpace(request.name) != request.name {
		return keyPairRequest{}, errKeyPairRequest
	}
	return request, nil
}

func jsonString(value json.RawMessage) (string, bool) {
	var text string
	if len(value) == 0 || value[0] != '"' || json.Unmarshal(value, &text) != nil {
		return "", false
	}
	return text, true
}
