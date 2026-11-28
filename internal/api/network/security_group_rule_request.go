package network

import (
	"encoding/json"
	"net/netip"
	"strconv"
	"strings"
	"unicode/utf8"

	domain "github.com/JSYoo5B/sandstack/internal/domain/network"
)

const (
	protocolNumberICMP     = 1
	protocolNumberTCP      = 6
	protocolNumberUDP      = 17
	protocolNumberIPv6ICMP = 58
)

var ruleProtocolNumbers = map[string]int{
	"icmp": protocolNumberICMP, "tcp": protocolNumberTCP, "udp": protocolNumberUDP,
	"ipv6-icmp": protocolNumberIPv6ICMP, "icmpv6": protocolNumberIPv6ICMP,
}

type createSecurityGroupRuleParams struct {
	groupID string
	rule    domain.SecurityGroupRule
	requestOwnership
}

func parseCreateSecurityGroupRule(data []byte) (createSecurityGroupRuleParams, *neutronError) {
	invalid := &neutronError{Type: "BadRequest", Message: "Expected one security_group_rule object with valid fields."}
	fields, err := readCreateFields(data, "security_group_rule")
	if err != nil {
		return createSecurityGroupRuleParams{}, invalid
	}
	var params createSecurityGroupRuleParams
	var minPort, maxPort *int
	var remoteIPPrefix, remoteGroupID *string
	for field, value := range fields {
		var target any
		nullable := false
		switch field {
		case "security_group_id":
			target = &params.groupID
		case "direction":
			target = &params.rule.Direction
		case "ethertype":
			target = &params.rule.EtherType
		case "protocol":
			continue
		case "remote_ip_prefix":
			target, nullable = &remoteIPPrefix, true
		case "remote_group_id":
			target, nullable = &remoteGroupID, true
		case "port_range_min":
			target, nullable = &minPort, true
		case "port_range_max":
			target, nullable = &maxPort, true
		case "description":
			target = &params.rule.Description
		case "project_id":
			target = &params.projectID
		case "tenant_id":
			target = &params.tenantID
		default:
			return createSecurityGroupRuleParams{}, invalid
		}
		if nullable {
			if json.Unmarshal(value, target) != nil {
				return createSecurityGroupRuleParams{}, invalid
			}
		} else if !decodeCreateField(value, target) {
			return createSecurityGroupRuleParams{}, invalid
		}
	}
	if !resourceUUIDPattern.MatchString(params.groupID) ||
		(params.rule.Direction != "ingress" && params.rule.Direction != "egress") ||
		utf8.RuneCountInString(params.rule.Description) > maxResourceNameLength {
		return createSecurityGroupRuleParams{}, invalid
	}
	params.groupID = strings.ToLower(params.groupID)
	params.rule.EtherType = canonicalRuleEtherType(params.rule.EtherType)
	if params.rule.EtherType == "" {
		return createSecurityGroupRuleParams{}, &neutronError{
			Type: "SecurityGroupRuleInvalidEtherType", Message: "The rule ethertype must be IPv4 or IPv6.",
		}
	}
	protocol, fault := parseRuleProtocol(fields["protocol"])
	if fault != nil {
		return createSecurityGroupRuleParams{}, fault
	}
	if fault := checkRulePortValues(minPort, maxPort); fault != nil {
		return createSecurityGroupRuleParams{}, fault
	}
	prefix, fault := parseRulePrefix(remoteIPPrefix)
	if fault != nil {
		return createSecurityGroupRuleParams{}, fault
	}
	params.rule.Protocol = canonicalRuleProtocol(protocol, params.rule.EtherType)
	params.rule.PortRangeMin, params.rule.PortRangeMax, fault = parseRulePorts(params.rule.Protocol, minPort, maxPort)
	if fault != nil {
		return createSecurityGroupRuleParams{}, fault
	}
	if prefix.IsValid() && prefix.Addr().Is4() != (params.rule.EtherType == "IPv4") {
		return createSecurityGroupRuleParams{}, &neutronError{
			Type: "SecurityGroupRuleParameterConflict", Message: "The remote IP prefix must match the rule ethertype.",
		}
	}
	if params.rule.EtherType == "IPv4" && ruleProtocolNumber(params.rule.Protocol) == protocolNumberIPv6ICMP {
		return createSecurityGroupRuleParams{}, &neutronError{
			Type: "SecurityGroupEthertypeConflictWithProtocol", Message: "IPv6 ICMP requires the IPv6 ethertype.",
		}
	}
	params.rule.RemoteIPPrefix = prefix
	if remoteGroupID != nil {
		params.rule.RemoteGroupID = *remoteGroupID
	}
	if prefix.IsValid() && params.rule.RemoteGroupID != "" {
		return createSecurityGroupRuleParams{}, &neutronError{
			Type:    "SecurityGroupMultipleRemoteEntites",
			Message: "Only one of remote_ip_prefix or remote_group_id or remote_address_group_id may be provided.",
		}
	}
	return params, nil
}

func parseRuleProtocol(value json.RawMessage) (string, *neutronError) {
	invalid := &neutronError{
		Type:    "SecurityGroupRuleInvalidProtocol",
		Message: "The rule protocol must be TCP, UDP, ICMP, IPv6 ICMP, or any protocol.",
	}
	if value == nil {
		return "", nil
	}
	var protocol *string
	if json.Unmarshal(value, &protocol) != nil {
		var number int
		if json.Unmarshal(value, &number) != nil {
			return "", invalid
		}
		text := strconv.Itoa(number)
		protocol = &text
	}
	if protocol == nil || *protocol == "any" {
		return "", nil
	}
	if number, err := strconv.Atoi(*protocol); err == nil {
		if strconv.Itoa(number) != *protocol || ruleProtocolNumber(*protocol) < 0 {
			return "", invalid
		}
		return *protocol, nil
	}
	name := strings.ToLower(*protocol)
	if _, supported := ruleProtocolNumbers[name]; !supported {
		return "", invalid
	}
	return name, nil
}

func ruleProtocolNumber(protocol string) int {
	if number, supported := ruleProtocolNumbers[protocol]; supported {
		return number
	}
	switch number, _ := strconv.Atoi(protocol); number {
	case protocolNumberICMP, protocolNumberTCP, protocolNumberUDP, protocolNumberIPv6ICMP:
		return number
	default:
		return -1
	}
}

func canonicalRuleProtocol(protocol, etherType string) string {
	if etherType != "IPv6" {
		return protocol
	}
	switch protocol {
	case "icmp", "icmpv6":
		return "ipv6-icmp"
	case strconv.Itoa(protocolNumberICMP):
		return strconv.Itoa(protocolNumberIPv6ICMP)
	default:
		return protocol
	}
}

func parseRulePrefix(value *string) (netip.Prefix, *neutronError) {
	if value == nil || *value == "" {
		return netip.Prefix{}, nil
	}
	prefix, err := netip.ParsePrefix(*value)
	if err != nil {
		if address, err := netip.ParseAddr(*value); err == nil && address.Zone() == "" {
			prefix = netip.PrefixFrom(address, address.BitLen())
		}
	}
	if !prefix.IsValid() {
		return netip.Prefix{}, &neutronError{Type: "InvalidCIDR", Message: "The rule requires an IPv4 or IPv6 address or CIDR."}
	}
	return prefix, nil
}

func checkRulePortValues(minimum, maximum *int) *neutronError {
	for _, port := range []*int{minimum, maximum} {
		if port != nil && (*port < 0 || *port > 65535) {
			return &neutronError{Type: "SecurityGroupInvalidPortValue", Message: "Port numbers must be between 0 and 65535."}
		}
	}
	return nil
}

func parseRulePorts(protocol string, minimum, maximum *int) (*int, *int, *neutronError) {
	if (minimum == nil && maximum == nil) || (minimum != nil && maximum != nil && *minimum == 1 && *maximum == 65535) {
		return nil, nil, nil
	}
	switch ruleProtocolNumber(protocol) {
	case protocolNumberTCP, protocolNumberUDP:
		if (minimum != nil && *minimum == 0) || (maximum != nil && *maximum == 0) {
			return nil, nil, &neutronError{
				Type: "SecurityGroupInvalidPortValue", Message: "Port numbers must be between 1 and 65535.",
			}
		}
		if minimum == nil || maximum == nil || *minimum > *maximum {
			return nil, nil, &neutronError{
				Type: "SecurityGroupInvalidPortRange", Message: "Supply both port endpoints in ascending order.",
			}
		}
	case protocolNumberICMP, protocolNumberIPv6ICMP:
		if (minimum != nil && *minimum > 255) || (maximum != nil && *maximum > 255) {
			return nil, nil, &neutronError{
				Type: "SecurityGroupInvalidIcmpValue", Message: "ICMP type and code must be between 0 and 255.",
			}
		}
		if minimum == nil {
			return nil, nil, &neutronError{
				Type: "SecurityGroupMissingIcmpType", Message: "An ICMP code requires an ICMP type.",
			}
		}
	default:
		return nil, nil, &neutronError{
			Type: "SecurityGroupProtocolRequiredWithPorts", Message: "A port range requires a protocol.",
		}
	}
	return minimum, maximum, nil
}
