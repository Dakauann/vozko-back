package copilottools

import (
	"strings"

	"vozko/domain/balance"
	"vozko/domain/shared"
)

const SearchPageSize = 10

const ReportQueuedNext = "o arquivo fica pronto em Relatórios; só quem pediu pode baixar"

func ClipText(s string, max int) string {
	text, cut := shared.TruncateRunes(strings.TrimSpace(s), max)
	if cut {
		return text + "…"
	}
	return text
}

func ReaisOf(rates balance.ExchangeRate) (func(micros int64) string, bool) {
	if rates == nil {
		return nil, false
	}
	rate, err := rates.CurrentRate()
	if err != nil || rate <= 0 {
		return nil, false
	}
	return func(micros int64) string { return shared.FormatBRL(shared.EstimateBRLFromMicros(micros, rate)) }, true
}
