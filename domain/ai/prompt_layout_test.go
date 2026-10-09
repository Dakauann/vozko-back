package ai

import (
	"strings"
	"testing"
)

func TestTheHistoryWindowStartMovesInBlocks(t *testing.T) {
	cases := []struct{ total, window, want int }{
		{30, 40, 0},
		{40, 40, 0},
		{41, 40, 10},
		{50, 40, 10},
		{51, 40, 20},
		{81, 80, 20},
		{100, 80, 20},
		{101, 80, 40},
		{3, 2, 1},
	}
	for _, tc := range cases {
		if got := HistoryWindowStart(tc.total, tc.window); got != tc.want {
			t.Fatalf("HistoryWindowStart(%d, %d) = %d, want %d", tc.total, tc.window, got, tc.want)
		}
	}
}

func TestAContextNoteIsAUserTurnThatSaysItIsNotFromAnyoneInTheConversation(t *testing.T) {
	note := ContextNote("  Base de conhecimento: horário de sábado 9h às 13h.  ", "", "Memórias: prefere WhatsApp.")
	if note.Role != RoleUser || !strings.HasPrefix(note.Content, contextNoteHeading) {
		t.Fatalf("note = %+v", note)
	}
	if !strings.Contains(note.Content, "\n\nBase de conhecimento: horário de sábado 9h às 13h.\n\nMemórias: prefere WhatsApp.") || strings.Contains(note.Content, "\n\n\n") {
		t.Fatalf("sections must be trimmed and blank ones dropped, got %q", note.Content)
	}
	if bare := ContextNote(); bare.Content != contextNoteHeading {
		t.Fatalf("a note with nothing to add still carries the heading for the clock, got %q", bare.Content)
	}
}
