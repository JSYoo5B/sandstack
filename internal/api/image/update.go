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
const maxImageTextLength = 255
const maxImageTags = 128

var errImagePatch = errors.New("expected one replace operation for /name, /tags, or /protected")
var errImageName = errors.New("the image name must be a string of up to 255 characters")
var errImageTags = errors.New("image tags must be an array of strings of up to 255 characters each")
var errImageTagLimit = errors.New("the image tag count exceeds the limit of 128")

func (h handler) updateImage(w http.ResponseWriter, r *http.Request, projectID string) {
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
	changes, err := parseImagePatch(data)
	if errors.Is(err, errImageTagLimit) {
		http.Error(w, err.Error(), http.StatusRequestEntityTooLarge)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	image, err := h.service.Update(projectID, chi.URLParam(r, "imageID"), changes)
	switch {
	case errors.Is(err, app.ErrImageNotFound):
		http.Error(w, "Image not found.", http.StatusNotFound)
	case err != nil:
		http.Error(w, "Unable to update image.", http.StatusInternalServerError)
	default:
		writeJSON(w, http.StatusOK, describeImage(image))
	}
}

func parseImagePatch(data []byte) (app.ImageUpdate, error) {
	var operations []json.RawMessage
	if !utf8.Valid(data) || json.Unmarshal(data, &operations) != nil || len(operations) != 1 {
		return app.ImageUpdate{}, errImagePatch
	}
	fields, err := readPatchOperation(operations[0])
	if err != nil {
		return app.ImageUpdate{}, err
	}
	var op, path string
	if len(fields) != 3 || json.Unmarshal(fields["op"], &op) != nil || op != "replace" ||
		json.Unmarshal(fields["path"], &path) != nil {
		return app.ImageUpdate{}, errImagePatch
	}
	switch path {
	case "/name":
		var name *string
		if json.Unmarshal(fields["value"], &name) != nil || name == nil || utf8.RuneCountInString(*name) > maxImageTextLength {
			return app.ImageUpdate{}, errImageName
		}
		return app.ImageUpdate{Name: name}, nil
	case "/tags":
		tags, err := parseImageTags(fields["value"])
		return app.ImageUpdate{Tags: tags}, err
	case "/protected":
		protected, err := parseImageProtected(fields["value"])
		return app.ImageUpdate{Protected: protected}, err
	default:
		return app.ImageUpdate{}, errImagePatch
	}
}

func parseImageProtected(raw json.RawMessage) (*bool, error) {
	var protected *bool
	if json.Unmarshal(raw, &protected) != nil || protected == nil {
		return nil, errors.New("the protected value must be a boolean")
	}
	return protected, nil
}

func readPatchOperation(data json.RawMessage) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return nil, errImagePatch
	}
	fields := make(map[string]json.RawMessage, 3)
	for decoder.More() {
		token, err := decoder.Token()
		key, ok := token.(string)
		if err != nil || !ok {
			return nil, errImagePatch
		}
		if _, duplicate := fields[key]; duplicate {
			return nil, errImagePatch
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, errImagePatch
		}
		fields[key] = value
	}
	if fields["value"] == nil {
		return nil, errImagePatch
	}
	return fields, nil
}

func parseImageTags(data json.RawMessage) ([]string, error) {
	var entries []*string
	if json.Unmarshal(data, &entries) != nil || entries == nil {
		return nil, errImageTags
	}
	tags := make([]string, len(entries))
	for i, entry := range entries {
		if entry == nil || utf8.RuneCountInString(*entry) > maxImageTextLength {
			return nil, errImageTags
		}
		tags[i] = *entry
	}
	if len(tags) > maxImageTags {
		return nil, errImageTagLimit
	}
	return tags, nil
}
