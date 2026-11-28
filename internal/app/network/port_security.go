package network

import (
	"errors"
	"fmt"
	"slices"

	domain "github.com/JSYoo5B/sandstack/internal/domain/network"
)

var (
	ErrPortHasSecurityGroup            = errors.New("port has security groups and port security cannot be disabled")
	ErrAddressPairRequiresPortSecurity = errors.New("port security must be enabled to have allowed address pairs")
	ErrDuplicateAddressPair            = errors.New("request contains a duplicate address pair")
)

type DuplicateAddressPairError struct{ Pair domain.AddressPair }

func (e *DuplicateAddressPairError) Error() string {
	return fmt.Sprintf(
		"%s: mac_address %s ip_address %s",
		ErrDuplicateAddressPair, e.Pair.MACAddress, e.Pair.IPAddress,
	)
}

func (e *DuplicateAddressPairError) Unwrap() error { return ErrDuplicateAddressPair }

type PortSecurity struct {
	Enabled             *bool
	AllowedAddressPairs *[]domain.AddressPair
}

func checkCreatePortSecurity(enabled bool, security PortSecurity, securityGroups *[]string) error {
	if enabled {
		return nil
	}
	if security.AllowedAddressPairs != nil && len(*security.AllowedAddressPairs) != 0 {
		return ErrAddressPairRequiresPortSecurity
	}
	if securityGroups != nil && len(*securityGroups) != 0 {
		return ErrPortSecurityRequired
	}
	return nil
}

func checkUpdatePortSecurity(port domain.Port, changes PortUpdate) error {
	enabled := !port.PortSecurityDisabled
	if changes.PortSecurityEnabled != nil {
		enabled = *changes.PortSecurityEnabled
	}
	if enabled {
		return nil
	}
	pairs := port.AllowedAddressPairs
	if changes.AllowedAddressPairs != nil {
		pairs = *changes.AllowedAddressPairs
	}
	if len(pairs) != 0 {
		return ErrAddressPairRequiresPortSecurity
	}
	if changes.SecurityGroups != nil {
		if len(*changes.SecurityGroups) != 0 {
			return ErrPortSecurityRequired
		}
	} else if len(port.SecurityGroups) != 0 {
		return ErrPortHasSecurityGroup
	}
	return nil
}

func resolveAddressPairs(requested []domain.AddressPair, portMAC string) ([]domain.AddressPair, error) {
	if len(requested) == 0 {
		return nil, nil
	}
	resolved := make([]domain.AddressPair, 0, len(requested))
	for _, pair := range requested {
		if pair.MACAddress == "" {
			pair.MACAddress = portMAC
		}
		if slices.Contains(resolved, pair) {
			return nil, &DuplicateAddressPairError{Pair: pair}
		}
		resolved = append(resolved, pair)
	}
	return resolved, nil
}
