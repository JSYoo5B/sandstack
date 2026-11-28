package network

import (
	"bytes"
	"encoding/json"
	"math"
	"strconv"
	"strings"
)

func pythonText(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) == nil && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return text
	}
	return pythonRepr(raw)
}

func pythonRepr(raw json.RawMessage) string {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var builder strings.Builder
	if writePythonValue(&builder, decoder) != nil {
		return string(raw)
	}
	return builder.String()
}

func writePythonValue(builder *strings.Builder, decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	switch value := token.(type) {
	case json.Delim:
		closing, open := "]", "["
		if value == '{' {
			closing, open = "}", "{"
		}
		builder.WriteString(open)
		for first := true; decoder.More(); first = false {
			if !first {
				builder.WriteString(", ")
			}
			if value == '{' {
				key, err := decoder.Token()
				if err != nil {
					return err
				}
				builder.WriteString(pythonString(key.(string)))
				builder.WriteString(": ")
			}
			if err := writePythonValue(builder, decoder); err != nil {
				return err
			}
		}
		if _, err := decoder.Token(); err != nil {
			return err
		}
		builder.WriteString(closing)
	case string:
		builder.WriteString(pythonString(value))
	case json.Number:
		builder.WriteString(pythonNumber(value))
	case bool:
		if value {
			builder.WriteString("True")
		} else {
			builder.WriteString("False")
		}
	case nil:
		builder.WriteString("None")
	}
	return nil
}

func pythonString(value string) string {
	quote := "'"
	if strings.Contains(value, "'") && !strings.Contains(value, `"`) {
		quote = `"`
	}
	replacer := strings.NewReplacer(`\`, `\\`, quote, `\`+quote, "\n", `\n`, "\r", `\r`, "\t", `\t`)
	return quote + replacer.Replace(value) + quote
}

func pythonNumber(number json.Number) string {
	text := number.String()
	if !strings.ContainsAny(text, ".eE") {
		return text
	}
	value, err := number.Float64()
	if err != nil || math.IsInf(value, 0) {
		return text
	}
	scientific := strconv.FormatFloat(value, 'e', -1, 64)
	mantissa, exponentText, _ := strings.Cut(scientific, "e")
	exponent, _ := strconv.Atoi(exponentText)
	if exponent >= -4 && exponent < 16 {
		fixed := strconv.FormatFloat(value, 'f', -1, 64)
		if !strings.Contains(fixed, ".") {
			fixed += ".0"
		}
		return fixed
	}
	sign := "+"
	if exponent < 0 {
		sign, exponent = "-", -exponent
	}
	return mantissa + "e" + sign + twoDigits(exponent)
}

func twoDigits(value int) string {
	if value < 10 {
		return "0" + strconv.Itoa(value)
	}
	return strconv.Itoa(value)
}
