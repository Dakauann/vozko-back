package telegram

import (
	"strings"
	"testing"

	"vozko/domain/conversation"
	tgdomain "vozko/domain/telegram"
)

func opts(pairs ...string) []conversation.InteractiveOption {
	out := make([]conversation.InteractiveOption, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, conversation.InteractiveOption{ID: pairs[i], Title: pairs[i+1]})
	}
	return out
}

func TestInlineKeyboardPutsOneButtonPerRow(t *testing.T) {
	rows, dropped := inlineKeyboardFor(opts("sim", "Sim", "nao", "Não"))

	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	for i, row := range rows {
		if len(row) != 1 {
			t.Errorf("row %d has %d buttons, want 1 — a multi-column layout truncates unbounded labels", i, len(row))
		}
	}
	if len(dropped) != 0 {
		t.Errorf("dropped = %v, want none", dropped)
	}
	// The callback payload IS the option id, which is what makes the reply
	// route to the option's own branch.
	if rows[0][0].CallbackData != "sim" || rows[0][0].Text != "Sim" {
		t.Errorf("button = %+v, want id as callback_data and title as text", rows[0][0])
	}
}

// callback_data is documented as "1-64 bytes". A truncated payload comes back
// as an id that matches no branch, which reads as the customer answering
// something unexpected — so the option is dropped instead.
func TestInlineKeyboardDropsAnOptionWhosePayloadOverflows(t *testing.T) {
	long := strings.Repeat("a", tgdomain.MaxCallbackDataBytes+1)
	rows, dropped := inlineKeyboardFor(opts("ok", "Certo", long, "Longo"))

	if len(rows) != 1 {
		t.Fatalf("rows = %d, want only the option that fits", len(rows))
	}
	if rows[0][0].CallbackData != "ok" {
		t.Errorf("kept %q, want the option that fits", rows[0][0].CallbackData)
	}
	if len(dropped) != 1 || !strings.Contains(dropped[0].reason, "64-byte") {
		t.Errorf("dropped = %+v, want the payload limit named", dropped)
	}
}

// Bytes, not characters: an id of accented text overflows sooner than it looks.
func TestInlineKeyboardMeasuresThePayloadInBytes(t *testing.T) {
	// 40 characters, 80 bytes — under 64 by rune count, over it by byte count.
	id := strings.Repeat("ç", 40)
	rows, dropped := inlineKeyboardFor(opts(id, "Acentuado"))

	if len(rows) != 0 {
		t.Error("an 80-byte id must be rejected even though it is only 40 characters")
	}
	if len(dropped) != 1 {
		t.Fatalf("dropped = %d, want 1", len(dropped))
	}
}

func TestInlineKeyboardFallsBackToTheIDWhenNoTitleIsGiven(t *testing.T) {
	rows, _ := inlineKeyboardFor(opts("sim", ""))
	if len(rows) != 1 || rows[0][0].Text != "sim" {
		t.Errorf("rows = %+v, want the id used as the visible label", rows)
	}
}

func TestInlineKeyboardSkipsAnOptionWithNoID(t *testing.T) {
	rows, dropped := inlineKeyboardFor(opts("", "Sem id"))
	if len(rows) != 0 {
		t.Error("an option with no id cannot report which branch to take")
	}
	if len(dropped) != 1 {
		t.Fatalf("dropped = %d, want 1", len(dropped))
	}
}

func TestInlineKeyboardStopsAtTheConfiguredCap(t *testing.T) {
	many := make([]conversation.InteractiveOption, 0, tgdomain.MaxInlineKeyboardButtons+5)
	for i := 0; i < tgdomain.MaxInlineKeyboardButtons+5; i++ {
		many = append(many, conversation.InteractiveOption{ID: string(rune('a'+i%26)) + string(rune('0'+i/26)), Title: "x"})
	}
	rows, dropped := inlineKeyboardFor(many)

	if len(rows) != tgdomain.MaxInlineKeyboardButtons {
		t.Errorf("rows = %d, want the cap %d", len(rows), tgdomain.MaxInlineKeyboardButtons)
	}
	if len(dropped) != 5 {
		t.Errorf("dropped = %d, want the 5 beyond the cap", len(dropped))
	}
}

// Telegram has no header or footer slot. Discarding the author's words would be
// silent data loss, so they are folded into the body.
func TestComposeInteractiveBodyKeepsHeaderAndFooter(t *testing.T) {
	body := composeInteractiveBody(conversation.SendInteractiveRequest{
		Header: "Atendimento",
		Body:   "Escolha uma opção",
		Footer: "Responda a qualquer momento",
	})

	for _, want := range []string{"Atendimento", "Escolha uma opção", "Responda a qualquer momento"} {
		if !strings.Contains(body, want) {
			t.Errorf("body %q is missing %q", body, want)
		}
	}
}

func TestComposeInteractiveBodyOmitsEmptyParts(t *testing.T) {
	if got := composeInteractiveBody(conversation.SendInteractiveRequest{Body: "Só o corpo"}); got != "Só o corpo" {
		t.Errorf("body = %q, want no stray separators", got)
	}
}

// The descriptor is the single source of truth for the numbers the editor shows
// and the adapter enforces.
func TestInteractiveLimitsComeFromTheDescriptor(t *testing.T) {
	caps := tgdomain.Descriptor().Capabilities
	a := &channelAdapter{caps: caps}

	limits := a.InteractiveLimits()
	if limits.MaxPayloadBytes != tgdomain.MaxCallbackDataBytes {
		t.Errorf("MaxPayloadBytes = %d, want %d", limits.MaxPayloadBytes, tgdomain.MaxCallbackDataBytes)
	}
	if !limits.PresentsChoices() {
		t.Error("Telegram must report that it can present choices")
	}
	if limits.SupportsOptionDescriptions {
		t.Error("inline buttons have no description slot")
	}
}
