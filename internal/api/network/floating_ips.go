package network

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	app "github.com/JSYoo5B/sandstack/internal/app/network"
	domain "github.com/JSYoo5B/sandstack/internal/domain/network"
)

const maxFloatingIPDescriptionLength = 255

var (
	errInvalidFloatingIPRequest = errors.New("expected one floatingip object with valid fields")
	errFloatingIPAddressPolicy  = errors.New("only administrators or project managers may set floating_ip_address")
	errFloatingIPSubnet         = errors.New("subnet_id selection is not supported")
)

type floatingIPBody struct {
	ID                string  `json:"id"`
	ProjectID         string  `json:"project_id"`
	TenantID          string  `json:"tenant_id"`
	Description       string  `json:"description"`
	FloatingNetworkID string  `json:"floating_network_id"`
	FloatingIPAddress string  `json:"floating_ip_address"`
	RouterID          *string `json:"router_id"`
	PortID            *string `json:"port_id"`
	FixedIPAddress    *string `json:"fixed_ip_address"`
	Status            string  `json:"status"`
}

type floatingIPResponse struct {
	FloatingIP floatingIPBody `json:"floatingip"`
}

func describeFloatingIP(floatingIP domain.FloatingIP) floatingIPBody {
	optional := func(value string) *string {
		if value == "" {
			return nil
		}
		return &value
	}
	var fixedIP *string
	if floatingIP.FixedIP.IsValid() {
		fixedIP = optional(floatingIP.FixedIP.String())
	}
	return floatingIPBody{
		ID:                floatingIP.ID,
		ProjectID:         floatingIP.ProjectID,
		TenantID:          floatingIP.ProjectID,
		Description:       floatingIP.Description,
		FloatingNetworkID: floatingIP.FloatingNetworkID,
		FloatingIPAddress: floatingIP.FloatingAddress.String(),
		RouterID:          optional(floatingIP.RouterID),
		PortID:            optional(floatingIP.PortID),
		FixedIPAddress:    fixedIP,
		Status:            floatingIP.Status,
	}
}

type floatingIPFields struct {
	networkID   *string
	description *string
	portID      *string
	fixedIP     netip.Addr
	requestOwnership
}

func parseFloatingIPFields(data []byte, create bool) (floatingIPFields, error) {
	var parsed floatingIPFields
	fields, err := readCreateFields(data, "floatingip")
	if err != nil {
		return parsed, errInvalidFloatingIPRequest
	}
	for field, value := range fields {
		null := bytes.Equal(value, []byte("null"))
		switch {
		case field == "port_id":
			portID := ""
			if !null && (!decodeCreateField(value, &portID) || !resourceUUIDPattern.MatchString(portID)) {
				return parsed, errInvalidFloatingIPRequest
			}
			portID = strings.ToLower(portID)
			parsed.portID = &portID
		case field == "fixed_ip_address":
			if null {
				continue
			}
			var text string
			if !decodeCreateField(value, &text) {
				return parsed, errInvalidFloatingIPRequest
			}
			if parsed.fixedIP, err = netip.ParseAddr(text); err != nil || parsed.fixedIP.Zone() != "" {
				return parsed, errInvalidFloatingIPRequest
			}
		case field == "description":
			var description string
			if !decodeCreateField(value, &description) ||
				utf8.RuneCountInString(description) > maxFloatingIPDescriptionLength {
				return parsed, errInvalidFloatingIPRequest
			}
			parsed.description = &description
		case create && field == "floating_network_id":
			var networkID string
			if !decodeCreateField(value, &networkID) || !resourceUUIDPattern.MatchString(networkID) {
				return parsed, errInvalidFloatingIPRequest
			}
			networkID = strings.ToLower(networkID)
			parsed.networkID = &networkID
		case create && field == "floating_ip_address":
			if !null {
				return parsed, errFloatingIPAddressPolicy
			}
		case create && field == "subnet_id":
			if !null {
				return parsed, errFloatingIPSubnet
			}
		case create && field == "project_id":
			if !decodeCreateField(value, &parsed.projectID) {
				return parsed, errInvalidFloatingIPRequest
			}
		case create && field == "tenant_id":
			if !decodeCreateField(value, &parsed.tenantID) {
				return parsed, errInvalidFloatingIPRequest
			}
		default:
			return parsed, errInvalidFloatingIPRequest
		}
	}
	if create && parsed.networkID == nil {
		return parsed, errInvalidFloatingIPRequest
	}
	return parsed, nil
}

func (h handler) createFloatingIP(w http.ResponseWriter, r *http.Request, projectID string) {
	data, ok := readCreateBody(w, r, "floatingip")
	if !ok {
		return
	}
	fields, err := parseFloatingIPFields(data, true)
	if err != nil {
		writeFloatingIPRequestError(w, err)
		return
	}
	if !fields.matches(projectID) {
		writeError(w, http.StatusBadRequest, "BadRequest", "Floating IP ownership must match the authenticated project.")
		return
	}
	request := app.FloatingIPCreate{NetworkID: *fields.networkID, FixedIP: fields.fixedIP}
	if fields.description != nil {
		request.Description = *fields.description
	}
	if fields.portID != nil {
		request.PortID = *fields.portID
	}
	floatingIP, err := h.service.CreateFloatingIP(projectID, request)
	if err != nil {
		writeFloatingIPError(w, "", request.PortID, err)
		return
	}
	writeJSON(w, http.StatusCreated, floatingIPResponse{FloatingIP: describeFloatingIP(floatingIP)})
}

func (h handler) listFloatingIPs(w http.ResponseWriter, r *http.Request, projectID string) {
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BadRequest", "The floating IP list query is malformed.")
		return
	}
	filter := app.FloatingIPListFilter{
		IDs:               query["id"],
		NetworkIDs:        query["floating_network_id"],
		PortIDs:           query["port_id"],
		RouterIDs:         query["router_id"],
		Statuses:          query["status"],
		FixedAddresses:    query["fixed_ip_address"],
		FloatingAddresses: query["floating_ip_address"],
	}
	for field := range query {
		switch field {
		case "id", "floating_network_id", "port_id", "router_id", "status", "fixed_ip_address",
			"floating_ip_address", "limit", "marker", "page_reverse":
		default:
			writeError(w, http.StatusBadRequest, "BadRequest",
				"The floating IP list query contains an unsupported parameter.")
			return
		}
	}
	options, err := parsePageOptions(query)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BadRequest", err.Error())
		return
	}
	page, err := h.service.ListFloatingIPsPage(projectID, filter, options)
	if err != nil {
		writeFloatingIPError(w, options.Marker, "", err)
		return
	}
	response := struct {
		FloatingIPs []floatingIPBody `json:"floatingips"`
		Links       []pageLink       `json:"floatingips_links,omitempty"`
	}{
		FloatingIPs: make([]floatingIPBody, len(page.Items)),
		Links:       h.pageLinks(r, Path+"/floatingips", query, page.NextMarker, page.PreviousMarker),
	}
	for i, floatingIP := range page.Items {
		response.FloatingIPs[i] = describeFloatingIP(floatingIP)
	}
	writeJSON(w, http.StatusOK, response)
}

func (h handler) showFloatingIP(w http.ResponseWriter, r *http.Request, projectID string) {
	floatingIPID := chi.URLParam(r, "floatingIPID")
	floatingIP, err := h.service.GetFloatingIP(projectID, floatingIPID)
	if err != nil {
		writeFloatingIPError(w, floatingIPID, "", err)
		return
	}
	writeJSON(w, http.StatusOK, floatingIPResponse{FloatingIP: describeFloatingIP(floatingIP)})
}

func (h handler) updateFloatingIP(w http.ResponseWriter, r *http.Request, projectID string) {
	data, ok := readCreateBody(w, r, "floatingip")
	if !ok {
		return
	}
	fields, err := parseFloatingIPFields(data, false)
	if err != nil {
		writeFloatingIPRequestError(w, err)
		return
	}
	floatingIPID := chi.URLParam(r, "floatingIPID")
	floatingIP, err := h.service.UpdateFloatingIP(projectID, floatingIPID, app.FloatingIPUpdate{
		Description: fields.description, PortID: fields.portID, FixedIP: fields.fixedIP,
	})
	if err != nil {
		portID := ""
		if fields.portID != nil {
			portID = *fields.portID
		}
		writeFloatingIPError(w, floatingIPID, portID, err)
		return
	}
	writeJSON(w, http.StatusOK, floatingIPResponse{FloatingIP: describeFloatingIP(floatingIP)})
}

func (h handler) deleteFloatingIP(w http.ResponseWriter, r *http.Request, projectID string) {
	if !requireEmptyDeleteBody(w, r) {
		return
	}
	floatingIPID := chi.URLParam(r, "floatingIPID")
	if err := h.service.DeleteFloatingIP(projectID, floatingIPID); err != nil {
		writeFloatingIPError(w, floatingIPID, "", err)
		return
	}
	writeNoContent(w)
}

func writeFloatingIPRequestError(w http.ResponseWriter, err error) {
	if errors.Is(err, errFloatingIPAddressPolicy) {
		writeError(w, http.StatusForbidden, "PolicyNotAuthorized", err.Error())
		return
	}
	writeError(w, http.StatusBadRequest, "BadRequest", err.Error())
}

func writeFloatingIPError(w http.ResponseWriter, floatingIPID, portID string, err error) {
	var missingNetwork *app.NetworkNotFoundError
	switch {
	case errors.Is(err, app.ErrFloatingIPNotFound):
		writeError(w, http.StatusNotFound, "FloatingIPNotFound",
			fmt.Sprintf("Floating IP %s could not be found.", floatingIPID))
	case errors.As(err, &missingNetwork):
		writeNetworkNotFound(w, missingNetwork.ID)
	case errors.Is(err, app.ErrPortNotFound):
		writePortError(w, portID, err)
	case errors.Is(err, app.ErrExternalGatewayForFloatingIPNotFound):
		writeError(w, http.StatusNotFound, "ExternalGatewayForFloatingIPNotFound", err.Error())
	case errors.Is(err, app.ErrFloatingIPPortAlreadyAssociated):
		writeError(w, http.StatusConflict, "FloatingIPPortAlreadyAssociated", err.Error())
	case errors.Is(err, app.ErrPortIPExhausted):
		writeError(w, http.StatusConflict, "IpAddressGenerationFailure", "The external subnet has no available addresses.")
	case errors.Is(err, app.ErrNetworkNotExternal), errors.Is(err, app.ErrFloatingIPNoIPv4Subnet),
		errors.Is(err, app.ErrFloatingIPFixedIPWithoutPort), errors.Is(err, app.ErrFloatingIPPortNoIPv4),
		errors.Is(err, app.ErrFloatingIPFixedIPNotOnPort):
		writeError(w, http.StatusBadRequest, "BadRequest", err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "HTTPInternalServerError", "Unable to process the floating IP request.")
	}
}
