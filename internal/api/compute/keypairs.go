package compute

import (
	"errors"
	"net/http"

	appcompute "github.com/JSYoo5B/sandstack/internal/app/compute"
	domaincompute "github.com/JSYoo5B/sandstack/internal/domain/compute"
)

const keyPairTimeFormat = "2006-01-02T15:04:05.000000"

type keyPairSummary struct {
	Fingerprint string `json:"fingerprint"`
	Name        string `json:"name"`
	PublicKey   string `json:"public_key"`
}

type createdKeyPair struct {
	keyPairSummary
	UserID     string `json:"user_id"`
	PrivateKey string `json:"private_key,omitempty"`
}

type keyPairDetail struct {
	keyPairSummary
	UserID    string  `json:"user_id"`
	ID        int     `json:"id"`
	CreatedAt string  `json:"created_at"`
	UpdatedAt *string `json:"updated_at"`
	Deleted   bool    `json:"deleted"`
	DeletedAt *string `json:"deleted_at"`
}

func summarizeKeyPair(pair domaincompute.KeyPair) keyPairSummary {
	return keyPairSummary{Fingerprint: pair.Fingerprint, Name: pair.Name, PublicKey: pair.PublicKey}
}

func (h handler) createKeyPair(w http.ResponseWriter, r *http.Request, scope requestScope) {
	data, ok := readRequestBody(w, r, "key pair")
	if !ok {
		return
	}
	request, err := parseKeyPairRequest(data)
	if err != nil {
		writeFault(w, http.StatusBadRequest, "badRequest", err.Error())
		return
	}

	var pair domaincompute.KeyPair
	var privateKey string
	if request.publicKey != nil {
		pair, err = h.keyPairs.Import(scope.userID, request.name, *request.publicKey)
	} else {
		pair, privateKey, err = h.keyPairs.Generate(scope.userID, request.name)
	}
	if err != nil {
		writeKeyPairError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		KeyPair createdKeyPair `json:"keypair"`
	}{KeyPair: createdKeyPair{keyPairSummary: summarizeKeyPair(pair), UserID: pair.UserID, PrivateKey: privateKey}})
}

func (h handler) listKeyPairs(w http.ResponseWriter, _ *http.Request, scope requestScope) {
	type item struct {
		KeyPair keyPairSummary `json:"keypair"`
	}
	pairs := h.keyPairs.List(scope.userID)
	response := struct {
		KeyPairs []item `json:"keypairs"`
	}{KeyPairs: make([]item, len(pairs))}
	for i, pair := range pairs {
		response.KeyPairs[i] = item{KeyPair: summarizeKeyPair(pair)}
	}
	writeJSON(w, http.StatusOK, response)
}

func (h handler) showKeyPair(w http.ResponseWriter, r *http.Request, scope requestScope) {
	pair, err := h.keyPairs.Get(scope.userID, pathParam(r, "keyPairName"))
	if err != nil {
		writeKeyPairError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		KeyPair keyPairDetail `json:"keypair"`
	}{KeyPair: keyPairDetail{
		keyPairSummary: summarizeKeyPair(pair),
		UserID:         pair.UserID,
		ID:             pair.ID,
		CreatedAt:      pair.CreatedAt.UTC().Format(keyPairTimeFormat),
	}})
}

func (h handler) deleteKeyPair(w http.ResponseWriter, r *http.Request, scope requestScope) {
	if err := h.keyPairs.Delete(scope.userID, pathParam(r, "keyPairName")); err != nil {
		writeKeyPairError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusAccepted)
}

func writeKeyPairError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, appcompute.ErrKeyPairNotFound):
		writeFault(w, http.StatusNotFound, "itemNotFound", "The key pair could not be found.")
	case errors.Is(err, appcompute.ErrKeyPairExists):
		writeFault(w, http.StatusConflict, "conflictingRequest", err.Error())
	case errors.Is(err, appcompute.ErrKeyPairQuota):
		writeFault(w, http.StatusForbidden, "forbidden", err.Error())
	case errors.Is(err, appcompute.ErrInvalidPublicKey):
		writeFault(w, http.StatusBadRequest, "badRequest", err.Error())
	default:
		writeFault(w, http.StatusInternalServerError, "computeFault", "Unable to process the key pair request.")
	}
}
