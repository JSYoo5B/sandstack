package network

import (
	"strings"

	domain "github.com/JSYoo5B/sandstack/internal/domain/network"
)

type PortListOrder int

const (
	PortOrderID PortListOrder = iota
	PortOrderNameAscending
	PortOrderNameDescending
)

func (o PortListOrder) compare(a, b domain.Port) int {
	switch o {
	case PortOrderNameAscending:
		if names := strings.Compare(a.Name, b.Name); names != 0 {
			return names
		}
	case PortOrderNameDescending:
		if names := strings.Compare(b.Name, a.Name); names != 0 {
			return names
		}
	}
	return strings.Compare(a.ID, b.ID)
}
