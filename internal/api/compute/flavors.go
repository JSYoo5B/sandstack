package compute

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/go-chi/chi/v5"

	appcompute "github.com/JSYoo5B/sandstack/internal/app/compute"
	domaincompute "github.com/JSYoo5B/sandstack/internal/domain/compute"
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
	writeJSON(w, http.StatusOK, struct {
		Flavor flavorBody `json:"flavor"`
	}{Flavor: h.describeFlavor(r, flavor, scope.pathProject)})
}

func (h handler) listFlavorsDetailed(w http.ResponseWriter, r *http.Request, scope requestScope) {
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		writeFault(w, http.StatusBadRequest, "badRequest", "The flavor list query is malformed.")
		return
	}

	var minRAM, minDisk int
	for parameter, values := range query {
		if len(values) != 1 || (parameter != "minRam" && parameter != "minDisk") {
			writeFault(w, http.StatusBadRequest, "badRequest", "Only one minRam and one minDisk query parameter are supported.")
			return
		}
		value, err := strconv.Atoi(values[0])
		if err != nil {
			writeFault(w, http.StatusBadRequest, "badRequest", "Flavor minimums must be integers.")
			return
		}
		if parameter == "minRam" {
			minRAM = value
		} else {
			minDisk = value
		}
	}

	results := h.service.ListFlavors(scope.projectID, minRAM, minDisk)
	flavors := make([]flavorBody, len(results))
	for index, flavor := range results {
		flavors[index] = h.describeFlavor(r, flavor, scope.pathProject)
	}
	writeJSON(w, http.StatusOK, struct {
		Flavors []flavorBody `json:"flavors"`
	}{Flavors: flavors})
}

func (h handler) describeFlavor(r *http.Request, flavor domaincompute.Flavor, pathProject string) flavorBody {
	projectPath := ""
	if pathProject != "" {
		projectPath = "/" + pathProject
	}
	resourcePath := projectPath + "/flavors/" + flavor.ID
	var swap any = flavor.SwapMiB
	if flavor.SwapMiB == 0 {
		swap = ""
	}
	return flavorBody{
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
}
