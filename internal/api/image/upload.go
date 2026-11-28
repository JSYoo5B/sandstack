package image

import (
	"errors"
	"io"
	"mime"
	"net/http"

	"github.com/go-chi/chi/v5"

	app "github.com/JSYoo5B/sandstack/internal/app/image"
)

const maxImageUploadBytes = 16 * 1024 * 1024

func (h handler) uploadImage(w http.ResponseWriter, r *http.Request, projectID string) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/octet-stream" {
		http.Error(w, "Image upload requires application/octet-stream.", http.StatusUnsupportedMediaType)
		return
	}
	imageID := chi.URLParam(r, "imageID")
	if err := h.service.CheckUpload(projectID, imageID); err != nil {
		writeUploadError(w, err)
		return
	}
	if r.ContentLength > maxImageUploadBytes {
		http.Error(w, "Image upload exceeds 16 MiB.", http.StatusRequestEntityTooLarge)
		return
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxImageUploadBytes))
	var sizeError *http.MaxBytesError
	if errors.As(err, &sizeError) {
		http.Error(w, "Image upload exceeds 16 MiB.", http.StatusRequestEntityTooLarge)
		return
	}
	if err != nil {
		http.Error(w, "Unable to read the image upload.", http.StatusBadRequest)
		return
	}
	if err := h.service.Upload(projectID, imageID, string(data)); err != nil {
		writeUploadError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeUploadError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, app.ErrImageNotFound):
		http.Error(w, "Image not found.", http.StatusNotFound)
	case errors.Is(err, app.ErrImageForbidden):
		http.Error(w, "You are not permitted to upload data for this image.", http.StatusForbidden)
	case errors.Is(err, app.ErrImageUploadState):
		http.Error(w, "Only queued images can receive an upload.", http.StatusConflict)
	case errors.Is(err, app.ErrImageUploadFormats):
		http.Error(w, "Image upload requires disk and container formats.", http.StatusBadRequest)
	default:
		http.Error(w, "Unable to save the image upload.", http.StatusInternalServerError)
	}
}
