package carriermessaging_usecase

import (
	"context"
	"errors"
	"strings"
	"testing"

	"vozko/domain/carriermessaging"
	"vozko/domain/rcs"
	"vozko/domain/sms"
)

func TestQuoteRCS(t *testing.T) {
	h := newHarness(t)
	q, err := h.svc.QuoteRCS(context.Background(), "ws-1", cardContent)
	if err != nil {
		t.Fatalf("QuoteRCS = %v", err)
	}
	if q.Kind != rcs.KindRichCard || q.Class != rcs.ClassSingle || q.VariableSlots != 1 || len(q.Issues) != 0 || q.Charge != carriermessaging.ChargeRCSSingle || q.UnitMicros != 63_333 {
		t.Fatalf("quote = %+v", q)
	}
	q, _ = h.svc.QuoteRCS(context.Background(), "ws-1", rcs.Content{Text: "Oi", Suggestions: []rcs.Suggestion{{Kind: rcs.SuggestDial, Label: "Ligar"}}})
	if len(q.Issues) != 1 || q.Issues[0].Path != "suggestions[0]" {
		t.Fatalf("issues = %+v", q.Issues)
	}
}

func TestQuoteSMSComposesWithTheRoutePrefix(t *testing.T) {
	h := newHarness(t)
	q, err := h.svc.QuoteSMS(context.Background(), "ws-1", "Oi {{1}}, "+strings.Repeat("a", 150), "route-1")
	if err != nil {
		t.Fatalf("QuoteSMS = %v", err)
	}
	if !strings.HasPrefix(q.Text, "Loja: Oi") || q.Measurement.Segments != 2 || q.TotalMicros != 70_000 || q.UnitMicros != 35_000 || q.VariableSlots != 1 || len(q.Issues) != 0 {
		t.Fatalf("quote = %+v", q)
	}
	q, _ = h.svc.QuoteSMS(context.Background(), "ws-1", "", "")
	if len(q.Issues) != 1 || !errors.Is(q.Issues[0], sms.ErrTextEmpty) {
		t.Fatalf("issues = %+v", q.Issues)
	}
	if _, err := h.svc.QuoteSMS(context.Background(), "ws-2", "Oi", "route-1"); !errors.Is(err, sms.ErrRouteNotFound) {
		t.Fatalf("other workspace route = %v", err)
	}
}
