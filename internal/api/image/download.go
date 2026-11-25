package image

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	app "github.com/JSYoo5B/sandstack/internal/app/image"
)

func (h handler) downloadImage(w http.ResponseWriter, r *http.Request, projectID string) {
	imageID := chi.URLParam(r, "imageID")
	size, err := h.service.CheckDownload(projectID, imageID)
	if err != nil {
		writeDownloadError(w, err)
		return
	}
	if len(r.Header.Values("Content-Range")) != 0 {
		http.Error(w, "Use Range to request part of an image.", http.StatusBadRequest)
		return
	}
	if r.URL.RawQuery != "" {
		http.Error(w, "Image downloads require an empty query string.", http.StatusBadRequest)
		return
	}
	selection, valid := resolveDownloadRange(w, r.Header.Values("Range"), size)
	if !valid {
		return
	}
	data, checksum, err := h.service.Download(projectID, imageID)
	if err != nil {
		writeDownloadError(w, err)
		return
	}
	writeImageData(w, data, checksum, selection)
}

type downloadRange struct {
	start int
	end   int
}

func resolveDownloadRange(w http.ResponseWriter, ranges []string, size int) (*downloadRange, bool) {
	if len(ranges) == 0 {
		return nil, true
	}
	if len(ranges) > 1 || strings.Contains(ranges[0], ",") {
		http.Error(w, "Image downloads accept one byte range per request.", http.StatusBadRequest)
		return nil, false
	}
	start, end, ok := parseByteRange(ranges[0], size)
	if !ok {
		w.Header().Set("Content-Range", "bytes */"+strconv.Itoa(size))
		http.Error(w, "Invalid or unsatisfiable byte range.", http.StatusRequestedRangeNotSatisfiable)
		return nil, false
	}
	return &downloadRange{start: start, end: end}, true
}

func writeDownloadError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, app.ErrImageNotFound):
		http.Error(w, "Image not found.", http.StatusNotFound)
	case errors.Is(err, app.ErrImageDeactivated):
		http.Error(w, "Image data is unavailable while the image is deactivated.", http.StatusForbidden)
	case errors.Is(err, app.ErrDownloadUnavailable):
		http.Error(w, "Image data is temporarily unavailable.", http.StatusServiceUnavailable)
	default:
		http.Error(w, "Unable to read image data.", http.StatusInternalServerError)
	}
}

func writeImageData(w http.ResponseWriter, data, checksum string, selection *downloadRange) {
	status := http.StatusOK
	switch {
	case selection != nil:
		status = http.StatusPartialContent
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", selection.start, selection.end, len(data)))
		data = data[selection.start : selection.end+1]
	case len(data) == 0:
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Header().Set("Content-MD5", checksum)
	w.WriteHeader(status)
	_, _ = io.WriteString(w, data)
}
