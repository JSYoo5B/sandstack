package compute

import (
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"

	appcompute "github.com/JSYoo5B/sandstack/internal/app/compute"
	appvolume "github.com/JSYoo5B/sandstack/internal/app/volume"
	domainvolume "github.com/JSYoo5B/sandstack/internal/domain/volume"
)

type attachmentBody struct {
	ID       string `json:"id"`
	VolumeID string `json:"volumeId"`
	ServerID string `json:"serverId"`
	Device   string `json:"device"`
}

type attachmentResponse struct {
	Attachment attachmentBody `json:"volumeAttachment"`
}

func describeAttachment(attachment domainvolume.Attachment) attachmentBody {
	return attachmentBody{
		ID:       attachment.VolumeID,
		VolumeID: attachment.VolumeID,
		ServerID: attachment.ServerID,
		Device:   attachment.Device,
	}
}

func (h handler) withAttachmentProject(next func(http.ResponseWriter, *http.Request, requestScope)) http.HandlerFunc {
	return h.withProject(func(w http.ResponseWriter, r *http.Request, scope requestScope) {
		if r.URL.RawQuery != "" {
			writeFault(w, http.StatusBadRequest, "badRequest", "Volume attachment operations require an empty query string.")
			return
		}
		next(w, r, scope)
	})
}

func (h handler) createAttachment(w http.ResponseWriter, r *http.Request, scope requestScope) {
	data, ok := readRequestBody(w, r, "attachment")
	if !ok {
		return
	}
	request, err := parseAttachmentRequest(data)
	if err != nil {
		writeFault(w, http.StatusBadRequest, "badRequest", err.Error())
		return
	}

	attachment, err := h.attachments.Attach(scope.projectID, chi.URLParam(r, "serverID"), request.volumeID, request.device)
	if err != nil {
		writeAttachmentError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, attachmentResponse{Attachment: describeAttachment(attachment)})
}

func (h handler) showAttachment(w http.ResponseWriter, r *http.Request, scope requestScope) {
	attachment, err := h.attachments.Get(scope.projectID, chi.URLParam(r, "serverID"), chi.URLParam(r, "volumeID"))
	if err != nil {
		writeAttachmentError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, attachmentResponse{Attachment: describeAttachment(attachment)})
}

func (h handler) listAttachments(w http.ResponseWriter, r *http.Request, scope requestScope) {
	attachments, err := h.attachments.List(scope.projectID, chi.URLParam(r, "serverID"))
	if err != nil {
		writeAttachmentError(w, err)
		return
	}
	response := struct {
		Attachments []attachmentBody `json:"volumeAttachments"`
	}{Attachments: make([]attachmentBody, len(attachments))}
	for i, attachment := range attachments {
		response.Attachments[i] = describeAttachment(attachment)
	}
	writeJSON(w, http.StatusOK, response)
}

func (h handler) deleteAttachment(w http.ResponseWriter, r *http.Request, scope requestScope) {
	var firstByte [1]byte
	if _, err := io.ReadFull(r.Body, firstByte[:]); err != io.EOF {
		writeFault(w, http.StatusBadRequest, "badRequest", "Detaching a volume requires an empty request body.")
		return
	}
	if err := h.attachments.Detach(scope.projectID, chi.URLParam(r, "serverID"), chi.URLParam(r, "volumeID")); err != nil {
		writeAttachmentError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusAccepted)
}

func writeAttachmentError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, appcompute.ErrServerNotFound):
		writeFault(w, http.StatusNotFound, "itemNotFound", "The server could not be found.")
	case errors.Is(err, appvolume.ErrVolumeNotFound):
		writeFault(w, http.StatusNotFound, "itemNotFound", "The volume could not be found.")
	case errors.Is(err, appvolume.ErrAttachmentNotFound):
		writeFault(w, http.StatusNotFound, "itemNotFound", "The volume attachment could not be found.")
	case errors.Is(err, appvolume.ErrVolumeStateConflict), errors.Is(err, appvolume.ErrAttachmentDeviceConflict),
		errors.Is(err, appcompute.ErrServerAttachmentState):
		writeFault(w, http.StatusConflict, "conflictingRequest", err.Error())
	case errors.Is(err, appvolume.ErrInvalidAttachmentDevice):
		writeFault(w, http.StatusBadRequest, "badRequest", err.Error())
	default:
		writeFault(w, http.StatusInternalServerError, "computeFault", "Unable to process the attachment request.")
	}
}
