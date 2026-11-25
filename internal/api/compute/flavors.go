package compute

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	appcompute "github.com/JSYoo5B/sandstack/internal/app/compute"
)

type flavorBody struct {
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	RAM        int            `json:"ram"`
	Disk       int            `json:"disk"`
	VCPUs      int            `json:"vcpus"`
	Swap       any            `json:"swap"`
	Ephemeral  int            `json:"OS-FLV-EXT-DATA:ephemeral"`
	Disabled   bool           `json:"OS-FLV-DISABLED:disabled"`
	Public     bool           `json:"os-flavor-access:is_public"`
	RxTxFactor float64        `json:"rxtx_factor"`
	Links      []resourceLink `json:"links"`
}

func (h handler) showFlavor(w http.ResponseWriter, r *http.Request, scope requestScope) {
	flavor, err := h.service.GetFlavor(scope.projectID, chi.URLParam(r, "flavorID"))
	if errors.Is(err, appcompute.ErrFlavorNotFound) {
		writeFault(w, http.StatusNotFound, "itemNotFound", "The flavor could not be found.")
		return
	}
	if err != nil {
		writeFault(w, http.StatusInternalServerError, "computeFault", "Unable to read the flavor.")
		return
	}
	projectPath := ""
	if scope.pathProject != "" {
		projectPath = "/" + scope.pathProject
	}
	resourcePath := projectPath + "/flavors/" + flavor.ID
	var swap any = flavor.SwapMiB
	if flavor.SwapMiB == 0 {
		swap = ""
	}
	body := flavorBody{
		ID:         flavor.ID,
		Name:       flavor.Name,
		RAM:        flavor.RAMMiB,
		Disk:       flavor.DiskGiB,
		VCPUs:      flavor.VCPUs,
		Swap:       swap,
		Ephemeral:  flavor.EphemeralGiB,
		Disabled:   flavor.Disabled,
		Public:     flavor.Public,
		RxTxFactor: flavor.RxTxFactor,
		Links: []resourceLink{
			{Rel: "self", Href: h.endpointURL(r, Path+resourcePath)},
			{Rel: "bookmark", Href: h.endpointURL(r, computeRoot+resourcePath)},
		},
	}
	writeJSON(w, http.StatusOK, struct {
		Flavor flavorBody `json:"flavor"`
	}{Flavor: body})
}
