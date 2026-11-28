package server

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	appcompute "github.com/JSYoo5B/sandstack/internal/app/compute"
	appidentity "github.com/JSYoo5B/sandstack/internal/app/identity"
	appimage "github.com/JSYoo5B/sandstack/internal/app/image"
	appnetwork "github.com/JSYoo5B/sandstack/internal/app/network"
	"github.com/JSYoo5B/sandstack/internal/app/sandbox"
	appvolume "github.com/JSYoo5B/sandstack/internal/app/volume"
)

func registerControlRoutes(
	router chi.Router,
	state *sandbox.State,
	adminToken string,
	register ...func(chi.Router),
) {
	router.Route("/_sandstack", func(router chi.Router) {
		router.Get("/health", noContent)
		router.Get("/ready", noContent)

		controls := router.With(requireAdminToken(adminToken))
		controls.Post("/reset", func(w http.ResponseWriter, r *http.Request) {
			state.Reset()
			noContent(w, r)
		})
		controls = controls.With(guardSandboxState(state))
		for _, route := range register {
			route(controls)
		}
	})
}

func registerIdentityControls(router chi.Router, service *appidentity.Service) {
	router.Post("/identity/invalidate-tokens", func(w http.ResponseWriter, r *http.Request) {
		service.InvalidateTokens()
		noContent(w, r)
	})
	router.Post("/identity/invalidate-token", func(w http.ResponseWriter, r *http.Request) {
		tokens := r.Header.Values("X-Subject-Token")
		if len(tokens) != 1 || strings.TrimSpace(tokens[0]) == "" {
			http.Error(w, "One X-Subject-Token header is required", http.StatusBadRequest)
			return
		}
		service.InvalidateToken(tokens[0])
		noContent(w, r)
	})
}

const maxConsoleOutput = 1 << 20

func registerComputeControls(router chi.Router, service *appcompute.Service) {
	const serverPath = "/compute/servers/{resourceID}"
	registerFixtureControl(router, serverPath+"/fault", "Server not found", service.SetFault)
	registerFixtureControl(router, serverPath+"/stale-detail", "Server not found", service.SetStaleDetail)
	registerFixtureControl(router, serverPath+"/unavailable-detail", "Server not found", service.SetUnavailableDetail)
	setConsole := func(w http.ResponseWriter, r *http.Request) {
		var output []byte
		if r.Method == http.MethodPut {
			var err error
			output, err = io.ReadAll(http.MaxBytesReader(w, r.Body, maxConsoleOutput))
			var sizeError *http.MaxBytesError
			if errors.As(err, &sizeError) {
				http.Error(w, "Console output must be at most 1 MiB", http.StatusRequestEntityTooLarge)
				return
			}
			if err != nil {
				http.Error(w, "Unable to read console output", http.StatusBadRequest)
				return
			}
		}
		if err := service.SetConsoleOutput(chi.URLParam(r, "resourceID"), output); err != nil {
			http.Error(w, "Server not found", http.StatusNotFound)
			return
		}
		noContent(w, r)
	}
	router.Put(serverPath+"/console-output", setConsole)
	router.Delete(serverPath+"/console-output", setConsole)
}

func registerNetworkControls(router chi.Router, service *appnetwork.Service) {
	registerFixtureControl(router, "/network/networks/{resourceID}/fault", "Network not found", service.SetDown)
}

func registerImageControls(router chi.Router, service *appimage.Service) {
	const imagePath = "/image/images/{resourceID}"
	registerFixtureControl(router, imagePath+"/deactivated", "Image not found", service.SetDeactivated)
	registerFixtureControl(router, imagePath+"/unavailable-download", "Image not found", service.SetUnavailableDownload)
}

func registerVolumeControls(router chi.Router, service *appvolume.Service) {
	const volumePath = "/volume/volumes/{resourceID}"
	registerFixtureControl(router, volumePath+"/unavailable-detail", "Volume not found", service.SetUnavailableDetail)
	setFault := func(w http.ResponseWriter, r *http.Request) {
		err := service.SetFailed(chi.URLParam(r, "resourceID"), r.Method == http.MethodPut)
		switch {
		case errors.Is(err, appvolume.ErrVolumeNotFound):
			http.Error(w, "Volume not found", http.StatusNotFound)
		case errors.Is(err, appvolume.ErrVolumeStateConflict):
			http.Error(w, "Volume fault cannot be changed in its current state", http.StatusConflict)
		case err != nil:
			http.Error(w, "Unable to change volume fault", http.StatusInternalServerError)
		default:
			noContent(w, r)
		}
	}
	router.Put(volumePath+"/fault", setFault)
	router.Delete(volumePath+"/fault", setFault)
	router.Put(volumePath+"/complete-create", func(w http.ResponseWriter, r *http.Request) {
		err := service.CompleteCreation(chi.URLParam(r, "resourceID"))
		switch {
		case errors.Is(err, appvolume.ErrVolumeNotFound):
			http.Error(w, "Volume not found", http.StatusNotFound)
		case errors.Is(err, appvolume.ErrVolumeStateConflict):
			http.Error(w, "Volume creation cannot be completed in its current state", http.StatusConflict)
		case err != nil:
			http.Error(w, "Unable to complete volume creation", http.StatusInternalServerError)
		default:
			noContent(w, r)
		}
	})
	registerSnapshotControls(router, service)
	registerBackupControls(router, service)
	registerVolumeExtensionControls(router, service)
}

func registerFixtureControl(router chi.Router, path, notFound string, set func(string, bool) error) {
	handle := func(w http.ResponseWriter, r *http.Request) {
		if err := set(chi.URLParam(r, "resourceID"), r.Method == http.MethodPut); err != nil {
			http.Error(w, notFound, http.StatusNotFound)
			return
		}
		noContent(w, r)
	}
	router.Put(path, handle)
	router.Delete(path, handle)
}

func requireAdminToken(adminToken string) func(http.Handler) http.Handler {
	expected := sha256.Sum256([]byte(adminToken))
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !validAdminToken(r.Header.Values("X-SandStack-Admin-Token"), expected) {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func validAdminToken(values []string, expected [sha256.Size]byte) bool {
	if len(values) != 1 {
		return false
	}
	provided := sha256.Sum256([]byte(values[0]))
	return subtle.ConstantTimeCompare(provided[:], expected[:]) == 1
}

func noContent(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}
