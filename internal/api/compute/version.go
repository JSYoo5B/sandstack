package compute

import (
	"net/http"
	"regexp"
	"strings"
)

const (
	baselineVersion     = "2.1"
	versionHeader       = "OpenStack-API-Version"
	legacyVersionHeader = "X-OpenStack-Nova-API-Version"
)

var microversionPattern = regexp.MustCompile(`^[1-9][0-9]*\.(0|[1-9][0-9]*)$`)

func acceptVersion(w http.ResponseWriter, r *http.Request) bool {
	version, valid := requestedVersion(r.Header)
	w.Header().Add("Vary", versionHeader)
	w.Header().Add("Vary", legacyVersionHeader)
	if !valid || (version != "" && version != "latest" && !microversionPattern.MatchString(version)) {
		writeFault(w, http.StatusBadRequest, "badRequest", "Invalid Compute API microversion.")
		return false
	}
	if version != "" && version != baselineVersion && version != "latest" {
		writeFault(w, http.StatusNotAcceptable, "computeFault", "This endpoint supports Compute API version 2.1.")
		return false
	}
	w.Header().Set(versionHeader, "compute "+baselineVersion)
	w.Header().Set(legacyVersionHeader, baselineVersion)
	return true
}

func requestedVersion(headers http.Header) (string, bool) {
	for _, value := range headers.Values(versionHeader) {
		for _, entry := range strings.Split(value, ",") {
			fields := strings.Fields(entry)
			if len(fields) == 0 || fields[0] != "compute" {
				continue
			}
			if len(fields) != 2 {
				return "", false
			}
			return fields[1], true
		}
	}
	return strings.TrimSpace(headers.Get(legacyVersionHeader)), true
}
