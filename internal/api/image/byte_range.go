package image

import (
	"errors"
	"strconv"
	"strings"
)

// parseByteRange returns inclusive bounds within a nonempty representation.
func parseByteRange(value string, size int) (start, end int, ok bool) {
	const spaces = " \t"
	unit, spec, ok := strings.Cut(value, "=")
	if size == 0 || !ok || !strings.EqualFold(strings.Trim(unit, spaces), "bytes") {
		return 0, 0, false
	}
	first, last, ok := strings.Cut(spec, "-")
	if !ok {
		return 0, 0, false
	}
	first = strings.Trim(first, spaces)
	last = strings.Trim(last, spaces)
	if first == "" {
		suffix, ok := boundedRangeNumber(last, size)
		if !ok || suffix == 0 {
			return 0, 0, false
		}
		return size - suffix, size - 1, true
	}
	start, ok = boundedRangeNumber(first, size)
	if !ok || start == size {
		return 0, 0, false
	}
	if last == "" {
		return start, size - 1, true
	}
	end, ok = boundedRangeNumber(last, size-1)
	if !ok || end < start {
		return 0, 0, false
	}
	return start, end, true
}

// Clamp valid decimals to the relevant limit before converting to int.
func boundedRangeNumber(text string, limit int) (int, bool) {
	if text == "" || strings.Trim(text, "0123456789") != "" {
		return 0, false
	}
	n, err := strconv.ParseUint(text, 10, 64)
	if errors.Is(err, strconv.ErrRange) {
		return limit, true
	}
	return int(min(n, uint64(limit))), err == nil
}
