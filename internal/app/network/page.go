package network

import "slices"

type PageOptions struct {
	Marker  string
	Limit   int
	Reverse bool
}

type Page[T any] struct {
	Items          []T
	NextMarker     string
	PreviousMarker string
}

// paginate orders a snapshot and finds the requested window without changing presentation order.
func paginate[T any](
	items []T,
	options PageOptions,
	marker *T,
	compare func(T, T) int,
	id func(T) string,
) Page[T] {
	slices.SortFunc(items, compare)
	if options.Limit <= 0 {
		return Page[T]{Items: items}
	}
	start, end := 0, len(items)
	if marker != nil {
		index, found := slices.BinarySearchFunc(items, *marker, compare)
		if options.Reverse {
			end = index
		} else {
			start = index
			if found {
				start++
			}
		}
	}
	if options.Reverse {
		start = max(0, end-options.Limit)
	} else {
		end = start + min(options.Limit, end-start)
	}
	page := Page[T]{Items: items[start:end]}
	if start < end {
		if end < len(items) {
			page.NextMarker = id(items[end-1])
		}
		if start > 0 {
			page.PreviousMarker = id(items[start])
		}
	}
	return page
}
