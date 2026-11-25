package image

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	app "github.com/JSYoo5B/sandstack/internal/app/image"
)

func (h handler) deleteImage(w http.ResponseWriter, r *http.Request, projectID string) {
	err := h.service.Delete(projectID, chi.URLParam(r, "imageID"))
	switch {
	case errors.Is(err, app.ErrImageNotFound):
		http.Error(w, "Image not found.", http.StatusNotFound)
	case err != nil:
		http.Error(w, "Unable to delete the image.", http.StatusInternalServerError)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}
