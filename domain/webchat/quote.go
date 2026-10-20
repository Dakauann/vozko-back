package webchat

import (
	"strings"

	"vozko/domain/conversation"
)

const MaxQuoteRunes = 140

type VisitorQuote struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

func QuoteOf(quoted *conversation.Message) *VisitorQuote {
	if quoted == nil || quoted.IsRetracted() {
		return nil
	}
	projected, visible := ProjectMessage(quoted)
	if !visible {
		return nil
	}
	line := strings.Join(strings.Fields(projected.Text), " ")
	if line == "" && projected.Media != nil {
		line = strings.TrimSpace(projected.Media.Filename)
	}
	if line == "" {
		return nil
	}
	return &VisitorQuote{ID: projected.ID, Text: clipLine(line)}
}

func clipLine(line string) string {
	runes := []rune(line)
	if len(runes) <= MaxQuoteRunes {
		return line
	}
	return strings.TrimSpace(string(runes[:MaxQuoteRunes-1])) + "…"
}
