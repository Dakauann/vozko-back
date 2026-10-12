package copilottools

import (
	"fmt"
	"strings"

	"vozko/domain/shared"
)

const SearchPageSize = 10

func ClipText(s string, max int) string {
	text, cut := shared.TruncateRunes(strings.TrimSpace(s), max)
	if cut {
		return text + "…"
	}
	return text
}

func FormatUSD(micros int64) string {
	return fmt.Sprintf("US$ %.2f", float64(micros)/1_000_000)
}
