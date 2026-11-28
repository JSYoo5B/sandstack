package network

import (
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"net/url"

	"github.com/go-chi/chi/v5"

	app "github.com/JSYoo5B/sandstack/internal/app/network"
	domain "github.com/JSYoo5B/sandstack/internal/domain/network"
)

type subnetBody struct {
	ID              string           `json:"id"`
	Name            string           `json:"name"`
	NetworkID       string           `json:"network_id"`
	ProjectID       string           `json:"project_id"`
	TenantID        string           `json:"tenant_id"`
	CIDR            string           `json:"cidr"`
	GatewayIP       *string          `json:"gateway_ip"`
	IPVersion       int              `json:"ip_version"`
	EnableDHCP      bool             `json:"enable_dhcp"`
	AllocationPools []allocationPool `json:"allocation_pools"`
	DNSNameservers  []string         `json:"dns_nameservers"`
	HostRoutes      []hostRouteBody  `json:"host_routes"`
	Tags            []string         `json:"tags"`
}

type hostRouteBody struct {
	Destination string `json:"destination"`
	NextHop     string `json:"nexthop"`
}

type allocationPool struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

type subnetResponse struct {
	Subnet subnetBody `json:"subnet"`
}

func describeSubnet(subnet domain.Subnet) subnetBody {
	var gateway *string
	if subnet.Gateway.IsValid() {
		address := subnet.Gateway.String()
		gateway = &address
	}
	nameservers := make([]string, len(subnet.DNSNameservers))
	for i, address := range subnet.DNSNameservers {
		nameservers[i] = address.String()
	}
	hostRoutes := make([]hostRouteBody, len(subnet.HostRoutes))
	for i, route := range subnet.HostRoutes {
		hostRoutes[i] = hostRouteBody{Destination: route.Destination.String(), NextHop: route.NextHop.String()}
	}
	pools := make([]allocationPool, len(subnet.AllocationPools))
	for i, pool := range subnet.AllocationPools {
		pools[i] = allocationPool{Start: pool.Start.String(), End: pool.End.String()}
	}
	return subnetBody{
		ID:              subnet.ID,
		Name:            subnet.Name,
		NetworkID:       subnet.NetworkID,
		ProjectID:       subnet.ProjectID,
		TenantID:        subnet.ProjectID,
		CIDR:            subnet.CIDR.String(),
		GatewayIP:       gateway,
		IPVersion:       4,
		EnableDHCP:      subnet.EnableDHCP,
		AllocationPools: pools,
		DNSNameservers:  nameservers,
		HostRoutes:      hostRoutes,
		Tags:            describeTags(subnet.Tags),
	}
}

func (h handler) createSubnet(w http.ResponseWriter, r *http.Request, projectID string) {
	data, ok := readCreateBody(w, r, "subnet")
	if !ok {
		return
	}
	params, err := parseCreateSubnet(data)
	if err != nil {
		writeSubnetRequestError(w, err)
		return
	}
	if !params.matches(projectID) {
		writeError(w, http.StatusBadRequest, "BadRequest", "Subnet ownership must match the authenticated project.")
		return
	}
	if params.ipVersion != 4 {
		writeError(w, http.StatusBadRequest, "InvalidInput", "This endpoint supports IPv4 subnets.")
		return
	}
	prefix, err := netip.ParsePrefix(params.cidr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "InvalidInput", "Expected a valid IPv4 CIDR.")
		return
	}

	subnet, err := h.service.CreateSubnet(projectID, params.networkID, app.SubnetCreate{
		Name:            params.name,
		CIDR:            prefix,
		EnableDHCP:      params.enableDHCP,
		Gateway:         params.gateway,
		DNSNameservers:  params.dnsNameservers,
		HostRoutes:      params.hostRoutes,
		AllocationPools: params.allocationPools,
	})
	if err != nil {
		if errors.Is(err, app.ErrNetworkNotFound) {
			writeNetworkNotFound(w, params.networkID)
		} else {
			writeSubnetError(w, "", err)
		}
		return
	}
	writeJSON(w, http.StatusCreated, subnetResponse{Subnet: describeSubnet(subnet)})
}

func (h handler) listSubnets(w http.ResponseWriter, r *http.Request, projectID string) {
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BadRequest", "the subnet list query is malformed")
		return
	}
	filter, err := parseSubnetListQuery(query)
	if err != nil {
		kind := "BadRequest"
		if errors.Is(err, errSubnetListCIDR) || errors.Is(err, errSubnetListDHCP) {
			kind = "InvalidInput"
		}
		writeError(w, http.StatusBadRequest, kind, err.Error())
		return
	}
	options, err := parsePageOptions(query)
	if err != nil {
		writeError(w, http.StatusBadRequest, "BadRequest", err.Error())
		return
	}
	page := h.service.ListSubnetsPage(projectID, filter, options)
	response := struct {
		Subnets []subnetBody `json:"subnets"`
		Links   []pageLink   `json:"subnets_links,omitempty"`
	}{
		Subnets: make([]subnetBody, len(page.Items)),
		Links:   h.pageLinks(r, Path+"/subnets", query, page.NextMarker, page.PreviousMarker),
	}
	for i, subnet := range page.Items {
		response.Subnets[i] = describeSubnet(subnet)
	}
	writeJSON(w, http.StatusOK, response)
}

func (h handler) showSubnet(w http.ResponseWriter, r *http.Request, projectID string) {
	subnetID := chi.URLParam(r, "subnetID")
	subnet, err := h.service.GetSubnet(projectID, subnetID)
	if err != nil {
		writeSubnetError(w, subnetID, err)
		return
	}
	writeJSON(w, http.StatusOK, subnetResponse{Subnet: describeSubnet(subnet)})
}

func (h handler) updateSubnet(w http.ResponseWriter, r *http.Request, projectID string) {
	data, ok := readCreateBody(w, r, "subnet")
	if !ok {
		return
	}
	changes, err := parseUpdateSubnet(data)
	if err != nil {
		writeSubnetRequestError(w, err)
		return
	}
	subnetID := chi.URLParam(r, "subnetID")
	subnet, err := h.service.UpdateSubnet(projectID, subnetID, changes)
	if err != nil {
		writeSubnetError(w, subnetID, err)
		return
	}
	writeJSON(w, http.StatusOK, subnetResponse{Subnet: describeSubnet(subnet)})
}

func writeSubnetRequestError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errInvalidNameservers), errors.Is(err, errInvalidHostRoutes),
		errors.Is(err, errInvalidSubnetPoolRequest):
		writeError(w, http.StatusBadRequest, "HTTPBadRequest", err.Error())
	case errors.Is(err, errNameserverIPVersion), errors.Is(err, errHostRouteIPVersion),
		errors.Is(err, app.ErrSubnetGateway):
		writeError(w, http.StatusBadRequest, "InvalidInput", err.Error())
	case errors.Is(err, errNameserverLimit):
		writeError(w, http.StatusBadRequest, "DNSNameServersExhausted", err.Error())
	case errors.Is(err, errHostRouteLimit):
		writeError(w, http.StatusBadRequest, "HostRoutesExhausted", err.Error())
	default:
		writeError(w, http.StatusBadRequest, "BadRequest", err.Error())
	}
}

func (h handler) deleteSubnet(w http.ResponseWriter, r *http.Request, projectID string) {
	if !requireEmptyDeleteBody(w, r) {
		return
	}
	subnetID := chi.URLParam(r, "subnetID")
	if err := h.service.DeleteSubnet(projectID, subnetID); err != nil {
		writeSubnetError(w, subnetID, err)
		return
	}
	writeNoContent(w)
}

func writeSubnetError(w http.ResponseWriter, subnetID string, err error) {
	switch {
	case errors.Is(err, app.ErrSubnetNotFound):
		writeError(w, http.StatusNotFound, "SubnetNotFound", fmt.Sprintf("Subnet %s could not be found.", subnetID))
	case errors.Is(err, app.ErrSubnetForbidden):
		writeError(w, http.StatusForbidden, "PolicyNotAuthorized", "The subnet is owned by another project.")
	case errors.Is(err, app.ErrSubnetInUse):
		writeError(w, http.StatusConflict, "SubnetInUse", "The subnet still has ports allocated to it.")
	case errors.Is(err, app.ErrSubnetCIDR), errors.Is(err, app.ErrSubnetOverlap), errors.Is(err, app.ErrSubnetGateway):
		writeError(w, http.StatusBadRequest, "InvalidInput", err.Error())
	case errors.Is(err, app.ErrSubnetAllocationPool):
		writeError(w, http.StatusBadRequest, "InvalidAllocationPool", err.Error())
	case errors.Is(err, app.ErrSubnetAllocationPoolBounds):
		writeError(w, http.StatusBadRequest, "OutOfBoundsAllocationPool", err.Error())
	case errors.Is(err, app.ErrSubnetAllocationPoolOverlap):
		writeError(w, http.StatusConflict, "OverlappingAllocationPools", err.Error())
	case errors.Is(err, app.ErrSubnetGatewayAttached):
		writeError(w, http.StatusConflict, "GatewayIpInUse", err.Error())
	case errors.Is(err, app.ErrSubnetAllocationPoolGateway):
		writeError(w, http.StatusConflict, "GatewayConflictWithAllocationPools", err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "HTTPInternalServerError", "Unable to process the subnet request.")
	}
}
