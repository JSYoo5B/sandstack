package image

import (
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	app "github.com/JSYoo5B/sandstack/internal/app/image"
)

func (h handler) downloadImage(w http.ResponseWriter, r *http.Request, projectID string) {
	data, checksum, err := h.service.Download(projectID, chi.URLParam(r, "imageID"))
	switch {
	case errors.Is(err, app.ErrImageNotFound):
		http.Error(w, "Image not found.", http.StatusNotFound)
	case errors.Is(err, app.ErrImageDeactivated):
		http.Error(w, "Image data is unavailable while the image is deactivated.", http.StatusForbidden)
	case err != nil:
		http.Error(w, "Unable to read image data.", http.StatusInternalServerError)
	case len(r.Header.Values("Range")) != 0 || len(r.Header.Values("Content-Range")) != 0:
		http.Error(w, "Image downloads require a complete file request.", http.StatusBadRequest)
	case r.URL.RawQuery != "":
		http.Error(w, "Image downloads require an empty query string.", http.StatusBadRequest)
	case data == "":
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.Header().Set("Content-MD5", checksum)
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, data)
	}
}
