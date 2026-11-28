package volume

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"unicode/utf8"

	app "github.com/JSYoo5B/sandstack/internal/app/volume"
)

var (
	errInvalidUploadImageAction = errors.New(
		"expected os-volume_upload_image with an image_name of 1 to 255 characters " +
			"and an optional force, disk_format, and container_format",
	)
	uploadDiskFormats = []string{"raw", "vmdk", "vdi", "qcow2", "vhd", "vhdx", "ploop"}
)

type uploadImageResponse struct {
	UploadImage uploadImageBody `json:"os-volume_upload_image"`
}

type uploadImageBody struct {
	ID                 string               `json:"id"`
	UpdatedAt          *string              `json:"updated_at"`
	Status             string               `json:"status"`
	DisplayDescription *string              `json:"display_description"`
	Size               int                  `json:"size"`
	VolumeType         uploadVolumeTypeBody `json:"volume_type"`
	ImageID            string               `json:"image_id"`
	ContainerFormat    *string              `json:"container_format"`
	DiskFormat         string               `json:"disk_format"`
	ImageName          string               `json:"image_name"`
	Protected          bool                 `json:"protected"`
	Visibility         string               `json:"visibility"`
}

type uploadVolumeTypeBody struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	IsPublic    bool              `json:"is_public"`
	ExtraSpecs  map[string]string `json:"extra_specs"`
	QOSSpecsID  *string           `json:"qos_specs_id"`
	Deleted     bool              `json:"deleted"`
	DeletedAt   *string           `json:"deleted_at"`
}

type uploadImageParams struct {
	request        app.UploadImageRequest
	invalidFormats bool
}

func parseUploadImage(data json.RawMessage) (uploadImageParams, error) {
	fields, err := readUniqueObject(data)
	if err != nil {
		return uploadImageParams{}, errInvalidUploadImageAction
	}
	var name *string
	if json.Unmarshal(fields["image_name"], &name) != nil || name == nil ||
		*name == "" || utf8.RuneCountInString(*name) > maxVolumeTextLength {
		return uploadImageParams{}, errInvalidUploadImageAction
	}
	params := uploadImageParams{request: app.UploadImageRequest{
		ImageName:       *name,
		ContainerFormat: "bare",
		DiskFormat:      "raw",
	}}
	for field, raw := range fields {
		switch field {
		case "image_name":
		case "force":
			force, ok := parseCinderBoolean(raw)
			if !ok {
				return uploadImageParams{}, errInvalidUploadImageAction
			}
			params.request.Force = force
		case "disk_format":
			var format *string
			if json.Unmarshal(raw, &format) != nil || format == nil || !slices.Contains(uploadDiskFormats, *format) {
				return uploadImageParams{}, errInvalidUploadImageAction
			}
			params.request.DiskFormat = *format
		case "container_format":
			var format *string
			if json.Unmarshal(raw, &format) != nil ||
				(format != nil && utf8.RuneCountInString(*format) > maxVolumeTextLength) {
				return uploadImageParams{}, errInvalidUploadImageAction
			}
			params.request.ContainerFormat = ""
			if format != nil {
				params.request.ContainerFormat = *format
				params.invalidFormats = *format == ""
			}
		default:
			return uploadImageParams{}, errInvalidUploadImageAction
		}
	}
	return params, nil
}

func (h handler) uploadVolumeImage(w http.ResponseWriter, projectID, volumeID string, data json.RawMessage) {
	params, err := parseUploadImage(data)
	if err != nil {
		writeFault(w, http.StatusBadRequest, "badRequest", err.Error())
		return
	}
	if params.invalidFormats {
		if _, err := h.service.Lookup(projectID, volumeID); err != nil {
			writeFault(w, http.StatusNotFound, "itemNotFound", "The volume could not be found.")
			return
		}
		writeFault(w, http.StatusBadRequest, "badRequest", "The image service rejected the container_format.")
		return
	}
	uploaded, err := h.service.UploadImage(projectID, volumeID, params.request)
	switch {
	case errors.Is(err, app.ErrVolumeNotFound):
		writeFault(w, http.StatusNotFound, "itemNotFound", "The volume could not be found.")
	case errors.Is(err, app.ErrVolumeStateConflict):
		writeFault(w, http.StatusBadRequest, "badRequest",
			"Invalid volume: Volume "+volumeID+" status must be available")
	case errors.Is(err, app.ErrUploadImageRejected):
		writeFault(w, http.StatusBadRequest, "badRequest",
			"The image service rejected the container_format or disk_format.")
	case err != nil:
		writeFault(w, http.StatusInternalServerError, "computeFault", "Unable to upload the volume to an image.")
	default:
		writeJSON(w, http.StatusAccepted, uploadImageResponse{UploadImage: describeUploadedImage(uploaded)})
	}
}

func describeUploadedImage(uploaded app.UploadedImage) uploadImageBody {
	volume, volumeType, image := uploaded.Volume, uploaded.VolumeType, uploaded.Image
	return uploadImageBody{
		ID:                 volume.ID,
		UpdatedAt:          optionalTimestamp(volume.UpdatedAt),
		Status:             "uploading",
		DisplayDescription: describeNullableText(volume.Description),
		Size:               volume.Size,
		VolumeType: uploadVolumeTypeBody{
			ID:          volumeType.ID,
			Name:        volumeType.Name,
			Description: volumeType.Description,
			IsPublic:    volumeType.Public,
			ExtraSpecs:  map[string]string{},
		},
		ImageID:         image.ID,
		ContainerFormat: optionalText(image.ContainerFormat),
		DiskFormat:      image.DiskFormat,
		ImageName:       image.Name,
		Protected:       image.Protected,
		Visibility:      "private",
	}
}
