package emaildesign

import (
	"strings"
	"testing"
)

func TestStarterKeys(t *testing.T) {
	want := []string{"blank", "newsletter", "promotion", "announcement", "event"}
	starters := StarterDocuments()
	if len(starters) != len(want) {
		t.Fatalf("expected %d starters, got %d", len(want), len(starters))
	}
	for i, key := range want {
		if starters[i].Key != key {
			t.Fatalf("starter %d: want %q, got %q", i, key, starters[i].Key)
		}
	}
}

func TestStartersAreValidAndRender(t *testing.T) {
	for _, starter := range StarterDocuments() {
		t.Run(starter.Key, func(t *testing.T) {
			if issues := Validate(starter.Document); len(issues) != 0 {
				t.Fatalf("starter has issues: %+v", issues)
			}
			for _, block := range starter.Document.Blocks {
				if block.Kind == KindImage {
					t.Fatal("starters must not depend on hosted images")
				}
			}
			out, err := Render(starter.Document, fullInput())
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			if !strings.Contains(out.HTML, "Descadastrar") {
				t.Fatal("starter render lacks the unsubscribe link")
			}
			if strings.Contains(out.HTML, "\u2014") || strings.Contains(out.HTML, "\u2013") {
				t.Fatal("starter copy contains a dash character that is not allowed")
			}
		})
	}
}

func TestStartersAreIndependentCopies(t *testing.T) {
	first := StarterDocuments()
	first[1].Document.Blocks[0].ID = "changed"
	second := StarterDocuments()
	if second[1].Document.Blocks[0].ID == "changed" {
		t.Fatal("starters share state between calls")
	}
}
