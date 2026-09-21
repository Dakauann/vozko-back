package lead_memory

import (
	"errors"
	"strings"
	"testing"

	"vozko/domain/actor"
)

func validMemory() *LeadMemory {
	return &LeadMemory{
		WorkspaceID: "ws-1",
		LeadID:      "lead-1",
		Category:    CategoryPreference,
		Content:     "Prefere boleto a PIX.",
		ActorKind:   actor.KindHuman,
		ActorID:     "user-1",
	}
}

func TestValidate(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*LeadMemory)
		wantErr error
	}{
		{name: "valid memory passes", mutate: func(m *LeadMemory) {}},
		{
			name:    "workspace is required",
			mutate:  func(m *LeadMemory) { m.WorkspaceID = "  " },
			wantErr: ErrWorkspaceRequired,
		},
		{
			name:    "lead is required",
			mutate:  func(m *LeadMemory) { m.LeadID = "" },
			wantErr: ErrLeadRequired,
		},
		{
			name:    "content is required",
			mutate:  func(m *LeadMemory) { m.Content = "" },
			wantErr: ErrContentRequired,
		},
		{
			name:    "content at the rune cap passes",
			mutate:  func(m *LeadMemory) { m.Content = strings.Repeat("ã", MaxContentLen) },
			wantErr: nil,
		},
		{
			name:    "content past the rune cap fails",
			mutate:  func(m *LeadMemory) { m.Content = strings.Repeat("ã", MaxContentLen+1) },
			wantErr: ErrContentTooLong,
		},
		{
			name:    "unknown category fails",
			mutate:  func(m *LeadMemory) { m.Category = "vibes" },
			wantErr: ErrInvalidCategory,
		},
		{
			name:    "actor is required",
			mutate:  func(m *LeadMemory) { m.ActorKind = "" },
			wantErr: ErrActorRequired,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := validMemory()
			tc.mutate(m)
			err := m.Validate()
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Validate() = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestNormalize(t *testing.T) {
	entry := "entry-1  "
	m := &LeadMemory{
		WorkspaceID:   "ws-1",
		LeadID:        "lead-1",
		Content:       "  Prefere boleto.  ",
		ActorKind:     actor.KindAI,
		ActorID:       "agent-1",
		SourceEntryID: &entry,
	}
	m.Normalize()

	if m.Content != "Prefere boleto." {
		t.Fatalf("content not trimmed: %q", m.Content)
	}
	if m.Category != CategoryOther {
		t.Fatalf("empty category should default to other, got %q", m.Category)
	}
	if m.ActorID != actor.FormatAI("agent-1") {
		t.Fatalf("actor id not canonicalized: %q", m.ActorID)
	}
	if m.SourceEntryID == nil || *m.SourceEntryID != "entry-1" {
		t.Fatalf("source entry not trimmed: %v", m.SourceEntryID)
	}

	empty := "   "
	m.SourceEntryType = &empty
	m.Normalize()
	if m.SourceEntryType != nil {
		t.Fatalf("blank source entry type should normalize to nil")
	}
}

func TestNormalizeContent(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Prefere  Boleto a PIX", "prefere boleto a pix"},
		{"  prefere boleto a pix  ", "prefere boleto a pix"},
		{"Prefere\tboleto\na  PIX", "prefere boleto a pix"},
	}
	for _, tc := range cases {
		if got := NormalizeContent(tc.in); got != tc.want {
			t.Fatalf("NormalizeContent(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	if NormalizeContent("prefere boleto") == NormalizeContent("prefere pix") {
		t.Fatal("different facts must not share a dedup key")
	}
}

func TestCategoryEnumParity(t *testing.T) {
	for _, c := range AllCategories() {
		if !c.Valid() {
			t.Fatalf("AllCategories lists invalid category %q", c)
		}
	}
	if Category("").Valid() || Category("unknown").Valid() {
		t.Fatal("Valid() accepted a category outside the closed set")
	}
	seen := map[Category]bool{}
	for _, c := range AllCategories() {
		if seen[c] {
			t.Fatalf("duplicate category %q in AllCategories", c)
		}
		seen[c] = true
	}
}
