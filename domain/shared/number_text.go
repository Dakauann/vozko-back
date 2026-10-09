package shared

import (
	"errors"
	"strings"
)

var ErrNumberTextForm = errors.New("number text must be a plain decimal with a dot mark")

func ParseNumberText(raw string) (float64, error) {
	text := strings.TrimSpace(raw)
	if strings.ContainsAny(text, ",xX_") {
		return 0, ErrNumberTextForm
	}
	return ParseDecimal(text)
}
