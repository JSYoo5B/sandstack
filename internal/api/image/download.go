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
	data, checksum, err := h.service.Download(projectID, chi.URLParam(r, "imageID"))
	switch {
	case errors.Is(err, app.ErrImageNotFound):
		http.Error(w, "Image not found.", http.StatusNotFound)
	case errors.Is(err, app.ErrImageDeactivated):
		http.Error(w, "Image data is unavailable while the image is deactivated.", http.StatusForbidden)
	case err != nil:
		http.Error(w, "Unable to read image data.", http.StatusInternalServerError)
	case len(r.Header.Values("Content-Range")) != 0:
		http.Error(w, "Use Range to request part of an image.", http.StatusBadRequest)
	case r.URL.RawQuery != "":
		http.Error(w, "Image downloads require an empty query string.", http.StatusBadRequest)
	default:
		writeImageData(w, data, checksum, r.Header.Values("Range"))
	}
}

func writeImageData(w http.ResponseWriter, data, checksum string, ranges []string) {
	size := len(data)
	status := http.StatusOK
	switch {
	case len(ranges) > 1 || (len(ranges) == 1 && strings.Contains(ranges[0], ",")):
		http.Error(w, "Image downloads accept one byte range per request.", http.StatusBadRequest)
		return
	case len(ranges) == 1:
		start, end, ok := parseByteRange(ranges[0], size)
		if !ok {
			w.Header().Set("Content-Range", "bytes */"+strconv.Itoa(size))
			http.Error(w, "Invalid or unsatisfiable byte range.", http.StatusRequestedRangeNotSatisfiable)
			return
		}
		status = http.StatusPartialContent
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, size))
		data = data[start : end+1]
	case size == 0:
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Header().Set("Content-MD5", checksum)
	w.WriteHeader(status)
	_, _ = io.WriteString(w, data)
}
