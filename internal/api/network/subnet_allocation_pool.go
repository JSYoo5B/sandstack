package network

import (
	"encoding/json"
	"errors"
	"net/netip"

	domain "github.com/JSYoo5B/sandstack/internal/domain/network"
)

var errInvalidSubnetPoolRequest = errors.New("expected an array of allocation pools with start and end IP addresses")

func parseSubnetAllocationPools(value json.RawMessage) ([]domain.AllocationPool, error) {
	var entries []json.RawMessage
	if json.Unmarshal(value, &entries) != nil {
		return nil, errInvalidSubnetPoolRequest
	}
	pools := make([]domain.AllocationPool, 0, len(entries))
	for _, entry := range entries {
		fields, err := readUniqueObject(entry)
		var start, end string
		if err != nil || len(fields) != 2 || !decodeCreateField(fields["start"], &start) ||
			!decodeCreateField(fields["end"], &end) {
			return nil, errInvalidSubnetPoolRequest
		}
		first, firstErr := netip.ParseAddr(start)
		last, lastErr := netip.ParseAddr(end)
		if firstErr != nil || lastErr != nil || first.Zone() != "" || last.Zone() != "" {
			return nil, errInvalidSubnetPoolRequest
		}
		pools = append(pools, domain.AllocationPool{Start: first, End: last})
	}
	return pools, nil
}
