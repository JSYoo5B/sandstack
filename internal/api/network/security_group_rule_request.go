package network

import (
	"encoding/json"
	"net/netip"
	"strings"
	"unicode/utf8"

	domain "github.com/JSYoo5B/sandstack/internal/domain/network"
)

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
	var remoteIPPrefix string
	for field, value := range fields {
		var target any
		switch field {
		case "security_group_id":
			target = &params.groupID
		case "direction":
			target = &params.rule.Direction
		case "ethertype":
			target = &params.rule.EtherType
		case "protocol":
			target = &params.rule.Protocol
		case "remote_ip_prefix":
			target = &remoteIPPrefix
		case "description":
			target = &params.rule.Description
		case "port_range_min":
			if json.Unmarshal(value, &minPort) != nil {
				return createSecurityGroupRuleParams{}, invalid
			}
			continue
		case "port_range_max":
			if json.Unmarshal(value, &maxPort) != nil {
				return createSecurityGroupRuleParams{}, invalid
			}
			continue
		case "project_id":
			target = &params.projectID
		case "tenant_id":
			target = &params.tenantID
		default:
			return createSecurityGroupRuleParams{}, invalid
		}
		if !decodeCreateField(value, target) {
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
	params.rule.Protocol = strings.ToLower(params.rule.Protocol)
	if params.rule.Protocol != "tcp" && params.rule.Protocol != "udp" {
		return createSecurityGroupRuleParams{}, &neutronError{
			Type: "SecurityGroupRuleInvalidProtocol", Message: "The rule protocol must be TCP or UDP.",
		}
	}
	prefix, fault := parseRulePrefix(remoteIPPrefix, params.rule.EtherType)
	if fault != nil {
		return createSecurityGroupRuleParams{}, fault
	}
	params.rule.RemoteIPPrefix = prefix
	minimum, maximum, fault := parseRulePorts(minPort, maxPort)
	if fault != nil {
		return createSecurityGroupRuleParams{}, fault
	}
	params.rule.PortRangeMin = minimum
	params.rule.PortRangeMax = maximum
	return params, nil
}

func parseRulePrefix(value, etherType string) (netip.Prefix, *neutronError) {
	prefix, err := netip.ParsePrefix(value)
	if err != nil {
		if address, err := netip.ParseAddr(value); err == nil && address.Zone() == "" {
			prefix = netip.PrefixFrom(address, address.BitLen())
		}
	}
	if !prefix.IsValid() {
		return netip.Prefix{}, &neutronError{Type: "InvalidCIDR", Message: "The rule requires an IPv4 or IPv6 address or CIDR."}
	}
	if prefix.Addr().Is4() != (etherType == "IPv4") {
		return netip.Prefix{}, &neutronError{
			Type: "SecurityGroupRuleParameterConflict", Message: "The remote IP prefix must match the rule ethertype.",
		}
	}
	return prefix, nil
}

func parseRulePorts(minimum, maximum *int) (int, int, *neutronError) {
	if minimum == nil && maximum == nil {
		return 0, 0, nil
	}
	if (minimum != nil && (*minimum < 1 || *minimum > 65535)) ||
		(maximum != nil && (*maximum < 1 || *maximum > 65535)) {
		return 0, 0, &neutronError{
			Type: "SecurityGroupInvalidPortValue", Message: "Port numbers must be between 1 and 65535.",
		}
	}
	if minimum == nil || maximum == nil || *minimum > *maximum {
		return 0, 0, &neutronError{
			Type: "SecurityGroupInvalidPortRange", Message: "Supply both port endpoints in ascending order.",
		}
	}
	if *minimum == 1 && *maximum == 65535 {
		return 0, 0, nil
	}
	return *minimum, *maximum, nil
}
