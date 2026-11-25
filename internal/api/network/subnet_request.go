package network

import (
	"encoding/json"
	"errors"
	"net/netip"
	"strings"

	app "github.com/JSYoo5B/sandstack/internal/app/network"
)

var errInvalidSubnetRequest = errors.New("expected one subnet object with valid fields")
var errInvalidSubnetName = errors.New("subnet name must be printable and contain at most 255 characters")
var errInvalidNameservers = errors.New("expected a list of unique DNS nameserver IP addresses")
var errNameserverIPVersion = errors.New("DNS nameservers on an IPv4 subnet must use IPv4 addresses")
var errNameserverLimit = errors.New("the number of DNS nameservers exceeds the limit 5")

type createSubnetParams struct {
	networkID      string
	name           string
	cidr           string
	ipVersion      int
	enableDHCP     bool
	dnsNameservers []netip.Addr
	requestOwnership
}

func parseCreateSubnet(data []byte) (createSubnetParams, error) {
	fields, err := readCreateFields(data, "subnet")
	if err != nil {
		return createSubnetParams{}, errInvalidSubnetRequest
	}
	params := createSubnetParams{enableDHCP: true}
	for field, value := range fields {
		var target any
		switch field {
		case "network_id":
			target = &params.networkID
		case "name":
			target = &params.name
		case "cidr":
			target = &params.cidr
		case "ip_version":
			target = &params.ipVersion
		case "enable_dhcp":
			target = &params.enableDHCP
		case "dns_nameservers":
			params.dnsNameservers, err = parseDNSNameservers(value)
			if err != nil {
				return createSubnetParams{}, err
			}
			continue
		case "project_id":
			target = &params.projectID
		case "tenant_id":
			target = &params.tenantID
		default:
			return createSubnetParams{}, errInvalidSubnetRequest
		}
		if !decodeCreateField(value, target) {
			return createSubnetParams{}, errInvalidSubnetRequest
		}
	}
	if !resourceUUIDPattern.MatchString(params.networkID) || params.cidr == "" || fields["ip_version"] == nil {
		return createSubnetParams{}, errors.New("expected a network UUID, CIDR, and IP version")
	}
	if !validResourceName(params.name) {
		return createSubnetParams{}, errInvalidSubnetName
	}
	params.networkID = strings.ToLower(params.networkID)
	return params, nil
}

func parseUpdateSubnet(data []byte) (app.SubnetUpdate, error) {
	fields, err := readCreateFields(data, "subnet")
	if err != nil {
		return app.SubnetUpdate{}, errInvalidSubnetRequest
	}
	var changes app.SubnetUpdate
	for field, value := range fields {
		var target any
		switch field {
		case "name":
			target = &changes.Name
		case "enable_dhcp":
			target = &changes.EnableDHCP
		case "dns_nameservers":
			changes.DNSNameservers, err = parseDNSNameservers(value)
			if err != nil {
				return app.SubnetUpdate{}, err
			}
			continue
		default:
			return app.SubnetUpdate{}, errInvalidSubnetRequest
		}
		if !decodeCreateField(value, target) {
			return app.SubnetUpdate{}, errInvalidSubnetRequest
		}
	}
	if changes.Name != nil && !validResourceName(*changes.Name) {
		return app.SubnetUpdate{}, errInvalidSubnetName
	}
	return changes, nil
}

func parseDNSNameservers(value json.RawMessage) ([]netip.Addr, error) {
	var nameservers []string
	if err := json.Unmarshal(value, &nameservers); err != nil {
		return nil, errInvalidNameservers
	}
	addresses := make([]netip.Addr, 0, len(nameservers))
	seen := make(map[netip.Addr]struct{}, len(nameservers))
	for _, address := range nameservers {
		ip, err := netip.ParseAddr(address)
		if err != nil || ip.Zone() != "" {
			return nil, errInvalidNameservers
		}
		if _, duplicate := seen[ip]; duplicate {
			return nil, errInvalidNameservers
		}
		seen[ip] = struct{}{}
		addresses = append(addresses, ip)
	}
	if len(addresses) > 5 {
		return nil, errNameserverLimit
	}
	for _, address := range addresses {
		if !address.Is4() {
			return nil, errNameserverIPVersion
		}
	}
	return addresses, nil
}
