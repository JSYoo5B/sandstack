package image

import (
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/go-chi/chi/v5"

	app "github.com/JSYoo5B/sandstack/internal/app/image"
	domain "github.com/JSYoo5B/sandstack/internal/domain/image"
)

type imageBody struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Owner           string    `json:"owner"`
	Status          string    `json:"status"`
	Visibility      string    `json:"visibility"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	ContainerFormat string    `json:"container_format"`
	DiskFormat      string    `json:"disk_format"`
	MinDisk         int       `json:"min_disk"`
	MinRAM          int       `json:"min_ram"`
	Protected       bool      `json:"protected"`
	Tags            []string  `json:"tags"`
	Checksum        *string   `json:"checksum"`
	Size            *int64    `json:"size"`
	Self            string    `json:"self"`
	File            string    `json:"file"`
	Schema          string    `json:"schema"`
}

func describeImage(image domain.Image) imageBody {
	status := "active"
	if image.Deactivated {
		status = "deactivated"
	}
	self := Path + "/images/" + url.PathEscape(image.ID)
	body := imageBody{
		ID:              image.ID,
		Name:            image.Name,
		Owner:           image.ProjectID,
		Status:          status,
		Visibility:      "private",
		CreatedAt:       image.CreatedAt,
		UpdatedAt:       image.UpdatedAt,
		ContainerFormat: "bare",
		DiskFormat:      "qcow2",
		Tags:            []string{},
		Self:            self,
		File:            self + "/file",
		Schema:          Path + "/schemas/image",
	}
	if image.Checksum != "" {
		body.Checksum = &image.Checksum
		body.Size = &image.Size
	}
	return body
}

func (h handler) showImage(w http.ResponseWriter, r *http.Request, projectID string) {
	image, err := h.service.Get(projectID, chi.URLParam(r, "imageID"))
	if errors.Is(err, app.ErrImageNotFound) {
		http.Error(w, "Image not found.", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "Unable to read image.", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, describeImage(image))
}
