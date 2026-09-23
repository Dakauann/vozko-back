package shared

import (
	"fmt"
	"math"
	"strings"
)

const brlAbsentValue = "-"

func FormatBRL(value float64) string {
	rounded := math.Round(value*1000) / 1000

	sign := ""
	if rounded < 0 {
		sign = "-"
		rounded = -rounded
	}

	whole := int64(rounded)
	fractional := int(math.Round((rounded - float64(whole)) * 1000))
	if fractional == 1000 {
		whole++
		fractional = 0
	}

	digits := strings.TrimRight(fmt.Sprintf("%03d", fractional), "0")
	if len(digits) < 2 {
		digits += strings.Repeat("0", 2-len(digits))
	}

	return fmt.Sprintf("%sR$ %s,%s", sign, formatBrazilianInteger(whole), digits)
}

func FormatMicrosBRL(micros int64, exchangeRate float64) string {
	if exchangeRate <= 0 {
		return brlAbsentValue
	}
	return FormatBRL(float64(micros) / 1_000_000 * exchangeRate)
}

func FormatMicrosUSD(micros int64) string {
	return fmt.Sprintf("%.6f", float64(micros)/1_000_000)
}

func formatBrazilianInteger(value int64) string {
	if value == 0 {
		return "0"
	}

	var parts []string
	for value > 0 {
		chunk := value % 1000
		value /= 1000
		if value > 0 {
			parts = append(parts, fmt.Sprintf("%03d", chunk))
		} else {
			parts = append(parts, fmt.Sprintf("%d", chunk))
		}
	}

	for left, right := 0, len(parts)-1; left < right; left, right = left+1, right-1 {
		parts[left], parts[right] = parts[right], parts[left]
	}

	return strings.Join(parts, ".")
}
