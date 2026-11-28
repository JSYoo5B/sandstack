package network

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"slices"
	"strings"

	domain "github.com/JSYoo5B/sandstack/internal/domain/network"
)

const maxAllowedAddressPairs = 10

var (
	errInvalidAddressPairs   = errors.New("invalid allowed address pairs")
	errAddressPairsExhausted = fmt.Errorf(
		"The number of allowed address pair exceeds the maximum %d.", maxAllowedAddressPairs,
	)
	errAddressPairMissingIP = errors.New("AllowedAddressPair must contain ip_address")
	errDuplicateAddressPair = errors.New("duplicate address pair in request")
	errInvalidPortSecurity  = errors.New("port_security_enabled must be a boolean")
)

var (
	ipv4Multicast = netip.MustParsePrefix("224.0.0.0/4")
	ipv6Multicast = netip.MustParsePrefix("ff00::/8")
)

func parsePortSecurityEnabled(value json.RawMessage) (*bool, error) {
	if value == nil {
		return nil, nil
	}
	var enabled bool
	if !decodeCreateField(value, &enabled) {
		return nil, errInvalidPortSecurity
	}
	return &enabled, nil
}

func parseAllowedAddressPairs(value json.RawMessage) (*[]domain.AddressPair, error) {
	if value == nil {
		return nil, nil
	}
	pairs := []domain.AddressPair{}
	if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
		return &pairs, nil
	}
	var entries []json.RawMessage
	if json.Unmarshal(value, &entries) != nil || entries == nil {
		return nil, fmt.Errorf("%w: Allowed address pairs must be a list.", errInvalidAddressPairs)
	}
	if len(entries) > maxAllowedAddressPairs {
		return nil, errAddressPairsExhausted
	}
	for _, entry := range entries {
		pair, err := parseAddressPair(entry)
		if err != nil {
			return nil, err
		}
		if slices.Contains(pairs, pair) {
			return nil, fmt.Errorf(
				"%w: mac_address %s ip_address %s",
				errDuplicateAddressPair, pair.MACAddress, pair.IPAddress,
			)
		}
		pairs = append(pairs, pair)
	}
	return &pairs, nil
}

func parseAddressPair(entry json.RawMessage) (domain.AddressPair, error) {
	fields, err := readUniqueObject(entry)
	if err != nil {
		return domain.AddressPair{}, fmt.Errorf("%w: '%s' is not a dictionary", errInvalidAddressPairs, entry)
	}
	var pair domain.AddressPair
	if value, ok := fields["mac_address"]; ok {
		if !decodeCreateField(value, &pair.MACAddress) || !validPairMAC(pair.MACAddress) {
			return domain.AddressPair{}, fmt.Errorf("%w: '%s' is not a valid MAC address", errInvalidAddressPairs, value)
		}
	}
	value, ok := fields["ip_address"]
	if !ok {
		return domain.AddressPair{}, errAddressPairMissingIP
	}
	if !decodeCreateField(value, &pair.IPAddress) {
		return domain.AddressPair{}, fmt.Errorf("%w: '%s' is not a valid IP address", errInvalidAddressPairs, value)
	}
	for field := range fields {
		if field != "mac_address" && field != "ip_address" {
			return domain.AddressPair{}, fmt.Errorf("%w: Unrecognized attribute(s) '%s'", errInvalidAddressPairs, field)
		}
	}
	if err := validatePairAddress(pair.IPAddress); err != nil {
		return domain.AddressPair{}, err
	}
	if pair.MACAddress != "" && multicastMAC(pair.MACAddress) {
		return domain.AddressPair{}, fmt.Errorf(
			"%w: MAC address %s is a multicast address, which is not supported.",
			errInvalidAddressPairs, pair.MACAddress,
		)
	}
	return pair, nil
}

func validPairMAC(text string) bool {
	mac, err := net.ParseMAC(text)
	if err != nil || len(mac) != 6 {
		return false
	}
	return !bytes.Equal(mac, make(net.HardwareAddr, 6)) &&
		!bytes.Equal(mac, net.HardwareAddr{0xff, 0xff, 0xff, 0xff, 0xff, 0xff})
}

func multicastMAC(text string) bool {
	mac, err := net.ParseMAC(text)
	return err == nil && mac[0]&0x01 != 0
}

func validatePairAddress(text string) error {
	if strings.Contains(text, "/") {
		prefix, err := netip.ParsePrefix(text)
		if err != nil || (prefix.Addr().Is4() && prefix.String() != text) {
			return fmt.Errorf("%w: '%s' is not a valid IP subnet", errInvalidAddressPairs, text)
		}
		if overlapsMulticast(prefix) {
			return fmt.Errorf(
				"%w: Subnet %s overlaps with multicast range, which is not supported.",
				errInvalidAddressPairs, text,
			)
		}
		return nil
	}
	address, err := netip.ParseAddr(text)
	if err != nil || address.Zone() != "" || (address.Is4() && address.String() != text) ||
		(!strings.Contains(text, ":") && strings.Count(text, ".") != 3) {
		return fmt.Errorf("%w: '%s' is not a valid IP address", errInvalidAddressPairs, text)
	}
	if address.IsMulticast() {
		return fmt.Errorf(
			"%w: IP %s is a multicast address, which is not supported.",
			errInvalidAddressPairs, text,
		)
	}
	return nil
}

func overlapsMulticast(prefix netip.Prefix) bool {
	multicast := ipv6Multicast
	if prefix.Addr().Is4() {
		if prefix.Bits() == 0 {
			return false
		}
		multicast = ipv4Multicast
	}
	bits := min(prefix.Bits(), multicast.Bits())
	ours, _ := prefix.Addr().Prefix(bits)
	theirs, _ := multicast.Addr().Prefix(bits)
	return ours == theirs
}
