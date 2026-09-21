package unofficial_whatsapp

import (
	"errors"
	"strings"
	"testing"
)

func validScript() SeedScript {
	return SeedScript{
		Bodies:      []string{"Oi {{1}}, tudo bem? Vi que voce se interessou no curso."},
		MaxMessages: 4,
	}
}

func TestSeedScriptNormalizeTrimsAndDropsBlankBodies(t *testing.T) {
	s := SeedScript{
		Bodies:      []string{"  Oi {{1}}  ", "", "   ", "\tOla {{1}}\n"},
		MaxMessages: 4,
		Context:     "  curso de enfermagem  ",
	}
	s.Normalize()

	if len(s.Bodies) != 2 || s.Bodies[0] != "Oi {{1}}" || s.Bodies[1] != "Ola {{1}}" {
		t.Fatalf("bodies = %#v, want the two non-blank ones trimmed", s.Bodies)
	}
	if s.Context != "curso de enfermagem" {
		t.Errorf("context = %q, want it trimmed", s.Context)
	}
}

func TestSeedScriptNormalizeDefaultsAndClampsTheMessageCap(t *testing.T) {
	s := SeedScript{Bodies: []string{"Oi"}}
	s.Normalize()
	if s.MaxMessages < ScriptMinMessages || s.MaxMessages > ScriptMaxMessages {
		t.Fatalf("default MaxMessages = %d, want it inside [%d,%d]",
			s.MaxMessages, ScriptMinMessages, ScriptMaxMessages)
	}

	over := SeedScript{Bodies: []string{"Oi"}, MaxMessages: 99}
	over.Normalize()
	if over.MaxMessages != ScriptMaxMessages {
		t.Errorf("MaxMessages 99 normalized to %d, want %d", over.MaxMessages, ScriptMaxMessages)
	}
	under := SeedScript{Bodies: []string{"Oi"}, MaxMessages: 1}
	under.Normalize()
	if under.MaxMessages != ScriptMinMessages {
		t.Errorf("MaxMessages 1 normalized to %d, want %d", under.MaxMessages, ScriptMinMessages)
	}
}

func TestSeedScriptNormalizeTruncatesTheContext(t *testing.T) {
	s := SeedScript{Bodies: []string{"Oi"}, Context: strings.Repeat("a", MaxScriptContextRunes+500)}
	s.Normalize()
	if got := len([]rune(s.Context)); got != MaxScriptContextRunes {
		t.Fatalf("context runes = %d, want %d", got, MaxScriptContextRunes)
	}
}

func TestSeedScriptValidate(t *testing.T) {
	tests := []struct {
		name   string
		script SeedScript
		want   error
	}{
		{"a normal script", validScript(), nil},
		{
			"no bodies at all",
			SeedScript{MaxMessages: 4},
			ErrScriptBodyRequired,
		},
		{
			"a blank body among real ones",
			SeedScript{Bodies: []string{"Oi {{1}}", "   "}, MaxMessages: 4},
			ErrScriptBodyEmpty,
		},
		{
			"a body past the channel's text limit",
			SeedScript{Bodies: []string{strings.Repeat("a", MaxTextRunes+1)}, MaxMessages: 4},
			ErrScriptBodyTooLong,
		},
		{
			"variants using different variables",
			SeedScript{Bodies: []string{"Oi {{1}}", "Oi {{2}}"}, MaxMessages: 4},
			ErrScriptVariantMismatch,
		},
		{
			"more variants than anyone proofreads",
			SeedScript{Bodies: manyBodies(MaxScriptVariants + 1), MaxMessages: 4},
			ErrScriptTooManyVariants,
		},
		{
			"a message cap outside the range",
			SeedScript{Bodies: []string{"Oi"}, MaxMessages: 99},
			ErrScriptMessageCountOutOfRange,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.script.Validate()
			if !errors.Is(err, tc.want) {
				t.Fatalf("Validate() = %v, want %v", err, tc.want)
			}
		})
	}
}

func manyBodies(n int) []string {
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, "Oi, variante")
	}
	return out
}

func TestSeedScriptNilIsSafe(t *testing.T) {
	var s *SeedScript
	s.Normalize()
	if err := s.Validate(); err != nil {
		t.Fatalf("a nil script failed validation: %v", err)
	}
}

func TestSeedScriptOpeningForIsDeterministicPerNumber(t *testing.T) {
	s := SeedScript{
		Bodies:      []string{"Oi {{1}}, variante A", "Oi {{1}}, variante B", "Oi {{1}}, variante C"},
		MaxMessages: 4,
	}
	s.Normalize()

	target := SeedTarget{Number: "5511999999999", Name: "Marina"}
	first := s.OpeningFor(target)
	for i := 0; i < 20; i++ {
		if got := s.OpeningFor(target); got != first {
			t.Fatalf("the same number got two openings: %q then %q", first, got)
		}
	}
	if !strings.Contains(first, "Marina") {
		t.Fatalf("opening = %q, want the name rendered into {{1}}", first)
	}
	if strings.Contains(first, "{{") {
		t.Fatalf("opening = %q, still carries a live placeholder", first)
	}
}

func TestSeedScriptOpeningForRendersOnlyTheName(t *testing.T) {
	s := SeedScript{Bodies: []string{"Oi {{1}}, aqui e a Ana. {{2}} segue aberto."}, MaxMessages: 4}
	s.Normalize()
	got := s.OpeningFor(SeedTarget{Number: "5511999999999", Name: "Marina"})
	if !strings.Contains(got, "Marina") || !strings.Contains(got, "{{2}}") {
		t.Fatalf("opening = %q, want {{1}} rendered and {{2}} untouched", got)
	}
}

func TestSeedScriptUsesName(t *testing.T) {
	with := SeedScript{Bodies: []string{"Oi, tudo bem?", "Oi {{1}}, tudo bem?"}}
	if !with.UsesName() {
		t.Error("a script with {{1}} in any body does not report using the name")
	}
	without := SeedScript{Bodies: []string{"Oi, tudo bem?", "Ola, tudo certo?"}}
	if without.UsesName() {
		t.Error("a script with no {{1}} reports using the name")
	}
}

func TestAcceptTurnsCapsTheThreadIncludingTheOperatorsOpening(t *testing.T) {
	s := SeedScript{Bodies: []string{"Oi"}, MaxMessages: 4}
	s.Normalize()

	got := s.AcceptTurns([]ScriptTurn{
		{FromLead: true, Text: "a"},
		{FromLead: false, Text: "b"},
		{FromLead: true, Text: "c"},
		{FromLead: false, Text: "d"},
		{FromLead: true, Text: "e"},
		{FromLead: false, Text: "f"},
	})
	if len(got) != 3 {
		t.Fatalf("accepted %d turns for a cap of 4, want 3", len(got))
	}
}

func TestAcceptTurnsEnforcesAlternationStartingWithTheLead(t *testing.T) {
	s := SeedScript{Bodies: []string{"Oi"}, MaxMessages: 8}
	s.Normalize()

	got := s.AcceptTurns([]ScriptTurn{
		{FromLead: false, Text: "our opening again"},
		{FromLead: true, Text: "lead one"},
		{FromLead: true, Text: "lead twice in a row"},
		{FromLead: false, Text: "ours"},
		{FromLead: true, Text: "lead again"},
	})

	want := []ScriptTurn{
		{FromLead: true, Text: "lead one"},
		{FromLead: false, Text: "ours"},
		{FromLead: true, Text: "lead again"},
	}
	if len(got) != len(want) {
		t.Fatalf("accepted %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("turn %d = %#v, want %#v", i, got[i], want[i])
		}
	}
}

func TestAcceptTurnsDropsEmptiesAndTrims(t *testing.T) {
	s := SeedScript{Bodies: []string{"Oi"}, MaxMessages: 8}
	s.Normalize()

	got := s.AcceptTurns([]ScriptTurn{
		{FromLead: true, Text: "   "},
		{FromLead: true, Text: "  oi, vi sim  "},
		{FromLead: false, Text: ""},
		{FromLead: false, Text: "Claro."},
	})
	if len(got) != 2 {
		t.Fatalf("accepted %d turns, want 2", len(got))
	}
	if got[0].Text != "oi, vi sim" || got[1].Text != "Claro." {
		t.Fatalf("turns = %#v, want them trimmed", got)
	}
}

func TestAcceptTurnsCapsEachLine(t *testing.T) {
	s := SeedScript{Bodies: []string{"Oi"}, MaxMessages: 8}
	s.Normalize()

	got := s.AcceptTurns([]ScriptTurn{{FromLead: true, Text: strings.Repeat("a", MaxScriptTurnRunes+200)}})
	if len(got) != 1 {
		t.Fatalf("accepted %d turns, want 1", len(got))
	}
	if runes := len([]rune(got[0].Text)); runes != MaxScriptTurnRunes {
		t.Fatalf("line is %d runes, want it capped at %d", runes, MaxScriptTurnRunes)
	}
}

func TestAcceptTurnsOnNothingReturnsNothing(t *testing.T) {
	s := SeedScript{Bodies: []string{"Oi"}, MaxMessages: 4}
	s.Normalize()
	if got := s.AcceptTurns(nil); len(got) != 0 {
		t.Fatalf("accepted %d turns from nothing", len(got))
	}
	if got := s.AcceptTurns([]ScriptTurn{{FromLead: false, Text: "ours"}}); len(got) != 0 {
		t.Fatalf("accepted %d turns where none alternated correctly", len(got))
	}
}

func TestScriptResponseSchemaDeclaresTheThreadShape(t *testing.T) {
	schema := ScriptResponseSchema(3)
	if schema["additionalProperties"] != false {
		t.Error("the schema allows additional properties at the top level")
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("the schema has no properties object")
	}
	threads, ok := props[ScriptSchemaKeyThreads].(map[string]any)
	if !ok {
		t.Fatalf("the schema has no %q array", ScriptSchemaKeyThreads)
	}
	if threads["type"] != "array" {
		t.Errorf("%q is %v, want an array", ScriptSchemaKeyThreads, threads["type"])
	}

	item := threads["items"].(map[string]any)
	itemProps := item["properties"].(map[string]any)
	turns := itemProps["turns"].(map[string]any)
	if turns["maxItems"] != 3 {
		t.Errorf("turns maxItems = %v, want 3", turns["maxItems"])
	}
	turnProps := turns["items"].(map[string]any)["properties"].(map[string]any)
	for _, key := range []string{"fromLead", "text"} {
		if _, ok := turnProps[key]; !ok {
			t.Errorf("a turn has no %q", key)
		}
	}
}
