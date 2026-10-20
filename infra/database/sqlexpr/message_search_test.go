package sqlexpr

import (
	"strings"
	"testing"
)

func TestMessageSearchReadsTheTextAndTheTranscriptAsOneExpression(t *testing.T) {
	if got, want := MessageSearchText("cm"), "LOWER(COALESCE(cm.text, '') || ' ' || COALESCE(cm.transcript, ''))"; got != want {
		t.Fatalf("MessageSearchText = %q, want %q", got, want)
	}
	if got, want := MessageSearchText(""), "LOWER(COALESCE(text, '') || ' ' || COALESCE(transcript, ''))"; got != want {
		t.Fatalf("the index needs the same expression without an alias: %q, want %q", got, want)
	}
}

func TestMessageTextMatchesSkipsDeletedAndRetractedMessagesWithOnePlaceholder(t *testing.T) {
	got := MessageTextMatches("cm_q")
	want := "cm_q.deleted_at IS NULL AND cm_q.retracted_at IS NULL AND " + MessageSearchText("cm_q") + " LIKE LOWER(?)"
	if got != want {
		t.Fatalf("MessageTextMatches = %q, want %q", got, want)
	}
	if n := strings.Count(got, "?"); n != 1 {
		t.Fatalf("placeholders = %d, want exactly one bound pattern", n)
	}
}
