package volume

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	app "github.com/JSYoo5B/sandstack/internal/app/volume"
	domain "github.com/JSYoo5B/sandstack/internal/domain/volume"
)

const transferCollection = "os-volume-transfer"

var (
	errInvalidTransferObject = errors.New("expected one transfer object with a volume_id and an optional name")
	errInvalidAcceptObject   = errors.New("expected one accept object with an auth_key")
	integerAuthKey           = regexp.MustCompile(`^-?(0|[1-9][0-9]*)$`)
)

type transferSummaryBody struct {
	ID       string       `json:"id"`
	VolumeID string       `json:"volume_id"`
	Name     *string      `json:"name"`
	Links    []volumeLink `json:"links"`
}

type transferDetailBody struct {
	ID        string       `json:"id"`
	CreatedAt string       `json:"created_at"`
	Name      *string      `json:"name"`
	VolumeID  string       `json:"volume_id"`
	Links     []volumeLink `json:"links"`
}

type transferCreatedBody struct {
	ID        string       `json:"id"`
	CreatedAt string       `json:"created_at"`
	Name      *string      `json:"name"`
	VolumeID  string       `json:"volume_id"`
	AuthKey   string       `json:"auth_key"`
	Links     []volumeLink `json:"links"`
}

func (h handler) transferLinks(r *http.Request, projectID, transferID string) []volumeLink {
	resourcePath := "/" + projectID + "/" + transferCollection + "/" + transferID
	return []volumeLink{
		{Href: h.endpointURL(r, Path+resourcePath), Rel: "self"},
		{Href: h.endpointURL(r, serviceRoot+resourcePath), Rel: "bookmark"},
	}
}

func (h handler) describeTransferSummary(r *http.Request, projectID string, transfer domain.Transfer) transferSummaryBody {
	return transferSummaryBody{
		ID:       transfer.ID,
		VolumeID: transfer.VolumeID,
		Name:     describeNullableText(transfer.Name),
		Links:    h.transferLinks(r, projectID, transfer.ID),
	}
}

func (h handler) describeTransferDetail(r *http.Request, projectID string, transfer domain.Transfer) transferDetailBody {
	return transferDetailBody{
		ID:        transfer.ID,
		CreatedAt: transfer.CreatedAt.UTC().Format(timestampLayout),
		Name:      describeNullableText(transfer.Name),
		VolumeID:  transfer.VolumeID,
		Links:     h.transferLinks(r, projectID, transfer.ID),
	}
}

func (h handler) createTransfer(w http.ResponseWriter, r *http.Request, projectID string) {
	data, ok := readJSONBody(w, r, "transfer")
	if !ok {
		return
	}
	volumeID, name, err := parseCreateTransfer(data)
	if err != nil {
		writeFault(w, http.StatusBadRequest, "badRequest", err.Error())
		return
	}

	created, err := h.service.CreateTransfer(projectID, volumeID, name)
	var snapshotErr app.TransferSnapshotStateError
	switch {
	case errors.Is(err, app.ErrVolumeNotFound):
		writeFault(w, http.StatusNotFound, "itemNotFound", "Volume "+volumeID+" could not be found.")
	case errors.Is(err, app.ErrTransferVolumeState):
		writeFault(w, http.StatusBadRequest, "badRequest", "Invalid volume: "+err.Error())
	case errors.As(err, &snapshotErr):
		writeFault(w, http.StatusBadRequest, "badRequest", "Invalid snapshot: "+snapshotErr.Error())
	case err != nil:
		writeFault(w, http.StatusInternalServerError, "computeFault", "Unable to create the transfer.")
	default:
		transfer := created.Transfer
		writeJSON(w, http.StatusAccepted, struct {
			Transfer transferCreatedBody `json:"transfer"`
		}{Transfer: transferCreatedBody{
			ID:        transfer.ID,
			CreatedAt: transfer.CreatedAt.UTC().Format(timestampLayout),
			Name:      describeNullableText(transfer.Name),
			VolumeID:  transfer.VolumeID,
			AuthKey:   created.AuthKey,
			Links:     h.transferLinks(r, projectID, transfer.ID),
		}})
	}
}

func (h handler) listTransfers(detail bool) projectHandler {
	return func(w http.ResponseWriter, r *http.Request, projectID string) {
		transfers := h.service.ListTransfers(projectID)
		bodies := make([]any, len(transfers))
		for i, transfer := range transfers {
			if detail {
				bodies[i] = h.describeTransferDetail(r, projectID, transfer)
			} else {
				bodies[i] = h.describeTransferSummary(r, projectID, transfer)
			}
		}
		writeJSON(w, http.StatusOK, struct {
			Transfers []any `json:"transfers"`
		}{Transfers: bodies})
	}
}

func (h handler) showTransfer(w http.ResponseWriter, r *http.Request, projectID string) {
	transferID := chi.URLParam(r, "transferID")
	transfer, err := h.service.GetTransfer(projectID, transferID)
	switch {
	case errors.Is(err, app.ErrTransferNotFound):
		writeTransferNotFound(w, transferID)
	case err != nil:
		writeFault(w, http.StatusInternalServerError, "computeFault", "Unable to read the transfer.")
	default:
		writeJSON(w, http.StatusOK, struct {
			Transfer transferDetailBody `json:"transfer"`
		}{Transfer: h.describeTransferDetail(r, projectID, transfer)})
	}
}

func (h handler) deleteTransfer(w http.ResponseWriter, r *http.Request, projectID string) {
	var firstByte [1]byte
	if _, err := io.ReadFull(r.Body, firstByte[:]); err != io.EOF {
		writeFault(w, http.StatusBadRequest, "badRequest", "Transfer deletion requires an empty request body.")
		return
	}

	transferID := chi.URLParam(r, "transferID")
	err := h.service.DeleteTransfer(projectID, transferID)
	switch {
	case errors.Is(err, app.ErrTransferNotFound):
		writeTransferNotFound(w, transferID)
	case err != nil:
		writeFault(w, http.StatusInternalServerError, "computeFault", "Unable to delete the transfer.")
	default:
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusAccepted)
	}
}

func (h handler) acceptTransfer(w http.ResponseWriter, r *http.Request, projectID string) {
	data, ok := readJSONBody(w, r, "accept")
	if !ok {
		return
	}
	authKey, err := parseAcceptTransfer(data)
	if err != nil {
		writeFault(w, http.StatusBadRequest, "badRequest", err.Error())
		return
	}

	transferID := chi.URLParam(r, "transferID")
	transfer, err := h.service.AcceptTransfer(projectID, transferID, authKey)
	switch {
	case errors.Is(err, app.ErrTransferNotFound):
		writeTransferNotFound(w, transferID)
	case errors.Is(err, app.ErrTransferAuthKey):
		writeFault(w, http.StatusBadRequest, "badRequest",
			"Invalid auth key: Attempt to transfer "+transferID+" with invalid auth key.")
	case errors.Is(err, app.ErrVolumeStateConflict):
		writeFault(w, http.StatusBadRequest, "badRequest",
			"Invalid volume: Transfer "+transferID+": Volume expected in awaiting-transfer state.")
	case err != nil:
		writeFault(w, http.StatusInternalServerError, "computeFault", "Unable to accept the transfer.")
	default:
		writeJSON(w, http.StatusAccepted, struct {
			Transfer transferSummaryBody `json:"transfer"`
		}{Transfer: h.describeTransferSummary(r, projectID, transfer)})
	}
}

func writeTransferNotFound(w http.ResponseWriter, transferID string) {
	writeFault(w, http.StatusNotFound, "itemNotFound", "Transfer "+transferID+" could not be found.")
}

func parseCreateTransfer(data []byte) (string, *string, error) {
	fields, err := readEnvelope(data, "transfer")
	if err != nil {
		return "", nil, errInvalidTransferObject
	}
	var volumeID string
	var name *string
	for field, value := range fields {
		switch field {
		case "volume_id":
			if json.Unmarshal(value, &volumeID) != nil || !resourceUUID.MatchString(volumeID) {
				return "", nil, errors.New("transfer volume_id must be a UUID")
			}
		case "name":
			if json.Unmarshal(value, &name) != nil {
				return "", nil, errors.New("transfer name must be a string or null")
			}
			if name != nil {
				trimmed := strings.TrimSpace(*name)
				if trimmed == "" || utf8.RuneCountInString(trimmed) > maxVolumeTextLength {
					return "", nil, errors.New("transfer name must contain 1 to 255 characters after trimming")
				}
				name = &trimmed
			}
		default:
			return "", nil, errInvalidTransferObject
		}
	}
	if volumeID == "" {
		return "", nil, errors.New("transfer volume_id must be a UUID")
	}
	return volumeID, name, nil
}

func parseAcceptTransfer(data []byte) (string, error) {
	fields, err := readEnvelope(data, "accept")
	if err != nil || len(fields) != 1 || fields["auth_key"] == nil {
		return "", errInvalidAcceptObject
	}
	value := fields["auth_key"]
	var authKey string
	if string(value) != "null" && json.Unmarshal(value, &authKey) == nil {
		return authKey, nil
	}
	if text := string(value); integerAuthKey.MatchString(text) {
		return text, nil
	}
	return "", errors.New("transfer auth_key must be a string or an integer")
}
