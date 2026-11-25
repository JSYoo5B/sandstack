package network

import (
	"encoding/json"
	"errors"
	"net/netip"

	app "github.com/JSYoo5B/sandstack/internal/app/network"
)

var errInvalidSubnetPoolRequest = errors.New("expected at most one allocation pool with start and end IP addresses")

func parseSubnetAllocationPool(value json.RawMessage) (*app.SubnetAllocationPool, error) {
	var pools []json.RawMessage
	if json.Unmarshal(value, &pools) != nil || len(pools) > 1 {
		return nil, errInvalidSubnetPoolRequest
	}
	if len(pools) == 0 {
		return nil, nil
	}
	fields, err := readUniqueObject(pools[0])
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
	return &app.SubnetAllocationPool{Start: first, End: last}, nil
}
