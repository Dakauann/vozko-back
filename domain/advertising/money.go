package advertising

import (
	"fmt"
	"strconv"
	"strings"
)

const microsPerUnit = 1_000_000

var zeroDecimalCurrencies = map[string]struct{}{
	"CLP": {}, "COP": {}, "CRC": {}, "HUF": {}, "ISK": {}, "IDR": {},
	"JPY": {}, "KRW": {}, "PYG": {}, "TWD": {}, "VND": {},
}

func NormalizeCurrency(code string) (string, error) {
	upper := strings.ToUpper(strings.TrimSpace(code))
	if len(upper) != 3 {
		return "", fmt.Errorf("%w: %q", ErrUnknownCurrency, code)
	}
	for _, r := range upper {
		if r < 'A' || r > 'Z' {
			return "", fmt.Errorf("%w: %q", ErrUnknownCurrency, code)
		}
	}
	return upper, nil
}

func currencyOffset(code string) (int64, error) {
	normalized, err := NormalizeCurrency(code)
	if err != nil {
		return 0, err
	}
	if _, ok := zeroDecimalCurrencies[normalized]; ok {
		return 1, nil
	}
	return 100, nil
}

func MinorToMicros(currency string, minor int64) (int64, error) {
	offset, err := currencyOffset(currency)
	if err != nil {
		return 0, err
	}
	return minor * (microsPerUnit / offset), nil
}

func MicrosToMinor(currency string, micros int64) (int64, error) {
	offset, err := currencyOffset(currency)
	if err != nil {
		return 0, err
	}
	return micros / (microsPerUnit / offset), nil
}

func DecimalToMicros(raw string) (int64, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0, fmt.Errorf("advertising: empty amount")
	}
	whole, fraction, _ := strings.Cut(trimmed, ".")
	if !digitsOnly(whole) || (fraction != "" && !digitsOnly(fraction)) || strings.Count(trimmed, ".") > 1 {
		return 0, fmt.Errorf("advertising: invalid amount %q", raw)
	}
	units, err := strconv.ParseInt(whole, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("advertising: invalid amount %q: %w", raw, err)
	}
	if len(fraction) > 6 {
		fraction = fraction[:6]
	}
	fraction += strings.Repeat("0", 6-len(fraction))
	parts, err := strconv.ParseInt(fraction, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("advertising: invalid amount %q: %w", raw, err)
	}
	micros := units*microsPerUnit + parts
	if micros == 0 && strings.Trim(trimmed, "0.") != "" {
		return 1, nil
	}
	return micros, nil
}

func digitsOnly(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func AmountToMinor(currency string, amount float64) (int64, error) {
	if amount <= 0 {
		return 0, fmt.Errorf("%w: %v", ErrInvalidBudget, amount)
	}
	micros, err := DecimalToMicros(strconv.FormatFloat(amount, 'f', 6, 64))
	if err != nil {
		return 0, err
	}
	return MicrosToMinor(currency, micros)
}

func MicrosToAmount(micros int64) float64 {
	return float64((micros+5_000)/10_000) / 100
}
