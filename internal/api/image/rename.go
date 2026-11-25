package image

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	app "github.com/JSYoo5B/sandstack/internal/app/image"
)

const imagePatchType = "application/openstack-images-v2.1-json-patch"
const maxImagePatchBody = 64 * 1024
const maxImageNameLength = 255

var errImageRenamePatch = errors.New("expected one replace /name operation with a string value of up to 255 characters")

func (h handler) renameImage(w http.ResponseWriter, r *http.Request, projectID string) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != imagePatchType {
		w.Header().Set("Accept-Patch", imagePatchType)
		http.Error(w, "Image PATCH requires "+imagePatchType+".", http.StatusUnsupportedMediaType)
		return
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxImagePatchBody))
	var sizeError *http.MaxBytesError
	if errors.As(err, &sizeError) {
		http.Error(w, "The image patch is too large.", http.StatusRequestEntityTooLarge)
		return
	}
	if err != nil {
		http.Error(w, "Unable to read the image patch.", http.StatusBadRequest)
		return
	}
	name, err := parseImageRename(data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	image, err := h.service.Rename(projectID, chi.URLParam(r, "imageID"), name)
	switch {
	case errors.Is(err, app.ErrImageNotFound):
		http.Error(w, "Image not found.", http.StatusNotFound)
	case err != nil:
		http.Error(w, "Unable to rename image.", http.StatusInternalServerError)
	default:
		writeJSON(w, http.StatusOK, describeImage(image))
	}
}

func parseImageRename(data []byte) (string, error) {
	var operations []json.RawMessage
	if !utf8.Valid(data) || json.Unmarshal(data, &operations) != nil || len(operations) != 1 {
		return "", errImageRenamePatch
	}
	decoder := json.NewDecoder(bytes.NewReader(operations[0]))
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return "", errImageRenamePatch
	}
	fields := make(map[string]string, 3)
	for decoder.More() {
		token, err := decoder.Token()
		key, ok := token.(string)
		if err != nil || !ok {
			return "", errImageRenamePatch
		}
		if _, duplicate := fields[key]; duplicate {
			return "", errImageRenamePatch
		}
		var value *string
		if err := decoder.Decode(&value); err != nil || value == nil {
			return "", errImageRenamePatch
		}
		fields[key] = *value
	}
	name, present := fields["value"]
	if len(fields) != 3 || fields["op"] != "replace" || fields["path"] != "/name" ||
		!present || utf8.RuneCountInString(name) > maxImageNameLength {
		return "", errImageRenamePatch
	}
	return name, nil
}
