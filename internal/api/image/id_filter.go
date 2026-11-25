package image

import (
	"errors"
	"regexp"
	"strings"
)

var errImageIDFilter = errors.New("the image ID filter must use equality or a valid in list")

// The first capture is a quoted operand and the third is an unquoted operand.
var imageIDOperands = regexp.MustCompile(`"([^"\\]*(\\.[^"\\]*)*)",?|([^,]+),?|,`)

func parseImageIDFilter(input string) (map[string]struct{}, error) {
	operator, value, explicit := strings.Cut(input, ":")
	switch {
	case !explicit:
		return map[string]struct{}{input: {}}, nil
	case operator == "eq":
		return map[string]struct{}{value: {}}, nil
	case operator != "in":
		return nil, errImageIDFilter
	}

	quoted := false
	for i, char := range value {
		if char != '"' || (i > 0 && value[i-1] == '\\') {
			continue
		}
		if quoted {
			if i+1 < len(value) && value[i+1] != ',' {
				return nil, errImageIDFilter
			}
		} else if i > 0 && value[i-1] != ',' {
			return nil, errImageIDFilter
		}
		quoted = !quoted
	}
	if quoted {
		return nil, errImageIDFilter
	}

	ids := make(map[string]struct{})
	for _, operand := range imageIDOperands.FindAllStringSubmatch(value, -1) {
		id := operand[1]
		if id == "" {
			id = operand[3]
		}
		ids[id] = struct{}{}
	}
	return ids, nil
}
