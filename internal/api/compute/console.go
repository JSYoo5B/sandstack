package compute

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"math/big"
	"net/http"
	"regexp"
	"strconv"

	"github.com/go-chi/chi/v5"

	appcompute "github.com/JSYoo5B/sandstack/internal/app/compute"
)

const consoleLengthFault = "Expected an os-getConsoleOutput object with an optional integer length of -1 or more."

var (
	consoleLengthPattern = regexp.MustCompile(`^-?[0-9]+$`)
	consoleControlChars  = regexp.MustCompile("[\x00-\x08\x0B-\x1F]")
)

func (h handler) showConsoleOutput(w http.ResponseWriter, r *http.Request, scope requestScope, data json.RawMessage) {
	length, valid := parseConsoleLength(data)
	if !valid {
		writeFault(w, http.StatusBadRequest, "badRequest", consoleLengthFault)
		return
	}

	output, err := h.service.ConsoleOutput(scope.projectID, chi.URLParam(r, "serverID"), length)
	switch {
	case errors.Is(err, appcompute.ErrServerNotFound):
		writeFault(w, http.StatusNotFound, "itemNotFound", "The server could not be found.")
	case err != nil:
		writeFault(w, http.StatusInternalServerError, "computeFault", "Unable to read the console output.")
	default:
		writeJSON(w, http.StatusOK, struct {
			Output string `json:"output"`
		}{Output: consoleControlChars.ReplaceAllString(output, "")})
	}
}

func parseConsoleLength(data json.RawMessage) (*int64, bool) {
	fields, err := readUniqueObject(data)
	if err != nil {
		return nil, false
	}
	value, ok := fields["length"]
	if len(fields) > 1 || (len(fields) == 1 && !ok) {
		return nil, false
	}
	if !ok || bytes.Equal(value, []byte("null")) {
		return nil, true
	}

	var text string
	if json.Unmarshal(value, &text) == nil {
		if !consoleLengthPattern.MatchString(text) {
			return nil, false
		}
		return consoleLength(new(big.Int).SetString(text, 10))
	}
	var number json.Number
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.UseNumber()
	if decoder.Decode(&number) != nil {
		return nil, false
	}
	if integer, ok := new(big.Int).SetString(number.String(), 10); ok {
		return consoleLength(integer, true)
	}
	float, err := strconv.ParseFloat(number.String(), 64)
	if err != nil || float != math.Trunc(float) {
		return nil, false
	}
	integer, _ := big.NewFloat(float).Int(nil)
	return consoleLength(integer, true)
}

func consoleLength(value *big.Int, ok bool) (*int64, bool) {
	if !ok || value.Cmp(big.NewInt(-1)) < 0 {
		return nil, false
	}
	length := int64(math.MaxInt64)
	if value.IsInt64() {
		length = value.Int64()
	}
	return &length, true
}
