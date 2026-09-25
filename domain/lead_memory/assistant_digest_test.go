package lead_memory

import (
	"strings"
	"testing"
	"time"
)

func TestDigestMemoryNamesTheAuthorAndTrimsLongNotes(t *testing.T) {
	at := time.Date(2026, 9, 20, 14, 30, 0, 0, time.UTC)
	d := DigestMemory(MemoryView{
		LeadMemory: &LeadMemory{ID: "m1", Category: CategoryPreference, Content: strings.Repeat("x", DigestContentRunes+5), UpdatedAt: at},
		ActorLabel: "Ana",
	})
	if d.MemoryID != "m1" || d.Category != "preference" || d.By != "Ana" || d.UpdatedAt != "2026-09-20T14:30:00Z" {
		t.Fatalf("digest = %+v", d)
	}
	if got := len([]rune(d.Content)); got != DigestContentRunes+1 {
		t.Fatalf("content runes = %d", got)
	}
}
