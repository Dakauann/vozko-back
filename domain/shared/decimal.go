package shared

import (
	"errors"
	"math"
	"strconv"
	"strings"
)

var ErrDecimalNotFinite = errors.New("decimal is not a finite number")

func ParseDecimal(raw string) (float64, error) {
	text := strings.TrimSpace(raw)
	if strings.Contains(text, ",") {
		text = strings.ReplaceAll(strings.ReplaceAll(text, ".", ""), ",", ".")
	}
	value, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return 0, err
	}
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, ErrDecimalNotFinite
	}
	return value, nil
}
