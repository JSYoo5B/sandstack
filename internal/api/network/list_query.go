package network

import "strings"

// parseBooleanFilters accepts the same values as Neutron's boolean converter.
func parseBooleanFilters(values []string) ([]bool, bool) {
	var states []bool
	for _, value := range values {
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "1", "t", "true", "on", "y", "yes":
			states = append(states, true)
		case "0", "f", "false", "off", "n", "no":
			states = append(states, false)
		default:
			return nil, false
		}
	}
	return states, true
}
