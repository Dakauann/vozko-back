package workflow

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"vozko/domain/tracepayload"
)

func scenarioGraph(inputs ...TestInput) Graph {
	g := pinnedGraph()
	g.FindNode("t").TestInputs = inputs
	return g
}

func scenario(id, name string, input map[string]interface{}) TestInput {
	return TestInput{ID: id, Name: name, Input: input, SavedAt: pinTime}
}

func TestSavedTestInputsAreValidatedLikePins(t *testing.T) {
	huge := map[string]interface{}{"body": strings.Repeat("a", tracepayload.MaxBytes+1)}
	cases := []struct {
		name  string
		graph func() Graph
		want  error
	}{
		{"two named scenarios on a trigger", func() Graph {
			return scenarioGraph(scenario("s1", "Completo", map[string]interface{}{"cpf": "1"}), scenario("s2", "Sem CPF", nil))
		}, nil},
		{"a scenario without a name", func() Graph { return scenarioGraph(scenario("s1", "  ", nil)) }, ErrTestInputInvalid},
		{"a scenario without an id", func() Graph { return scenarioGraph(scenario("", "Completo", nil)) }, ErrTestInputInvalid},
		{"two scenarios with the same name", func() Graph {
			return scenarioGraph(scenario("s1", "Completo", nil), scenario("s2", " Completo ", nil))
		}, ErrTestInputInvalid},
		{"two scenarios with the same id", func() Graph {
			return scenarioGraph(scenario("s1", "A", nil), scenario("s1", "B", nil))
		}, ErrTestInputInvalid},
		{"an input over the step payload cap", func() Graph { return scenarioGraph(scenario("s1", "Grande", huge)) }, ErrTestInputTooLarge},
		{"a scenario on an action node", func() Graph {
			g := pinnedGraph()
			g.FindNode("http").TestInputs = []TestInput{scenario("s1", "A", nil)}
			return g
		}, ErrTestInputNotAllowed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := tc.graph()
			if err := ValidateTestInputs(&g); !errors.Is(err, tc.want) {
				t.Fatalf("ValidateTestInputs = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestATestInputErrorIsAValidationErrorTheEditorCanShow(t *testing.T) {
	g := scenarioGraph(scenario("s1", "", nil))
	err := (&Workflow{WorkspaceID: "ws1", Name: "Fluxo", Status: WorkflowStatusDraft, Type: WorkflowTypeMessages, Graph: g}).Validate()
	if !errors.Is(err, ErrTestInputInvalid) || !IsValidationError(err) {
		t.Fatalf("Validate = %v, want a validation error for the scenario", err)
	}
}

func TestStripDraftOnlyRemovesPinsAndScenariosButKeepsTheDraft(t *testing.T) {
	g := scenarioGraph(scenario("s1", "Completo", map[string]interface{}{"cpf": "1"}))
	if err := SetPin(&g, "http", NodePin{Output: map[string]interface{}{"a": 1.0}, Source: PinSourceManual, PinnedAt: pinTime}); err != nil {
		t.Fatal(err)
	}
	stripped := StripDraftOnly(g)
	if stripped.HasDraftOnly() || stripped.HasPins() {
		t.Fatal("a published version must carry neither pins nor saved test inputs")
	}
	if !g.HasDraftOnly() || len(g.FindNode("t").TestInputs) != 1 || g.FindNode("http").Pinned == nil {
		t.Fatal("stripping for publish must keep the draft's test data")
	}
	encoded, _ := json.Marshal(stripped)
	if strings.Contains(string(encoded), "testInputs") {
		t.Fatalf("the stripped graph must not serialise test inputs: %s", encoded)
	}
}

func TestGraphHashIgnoresSavedTestInputs(t *testing.T) {
	g := pinnedGraph()
	before := GraphHash(&g)
	g.FindNode("t").TestInputs = []TestInput{scenario("s1", "Completo", map[string]interface{}{"cpf": "1"})}
	if GraphHash(&g) != before {
		t.Fatal("a saved test input is test data, it must not change the publish hash")
	}
}

func TestTestInputsTravelInTheGraphJSONAndCloneDeeply(t *testing.T) {
	raw := `{"nodes":[{"id":"t","type":"trigger_agent_call","position":{"x":0,"y":0},"config":{},"testInputs":[{"id":"s1","name":"Completo","input":{"cpf":"1"},"savedAt":"2026-10-20T12:00:00Z"}]}],"edges":[]}`
	var g Graph
	if err := json.Unmarshal([]byte(raw), &g); err != nil {
		t.Fatal(err)
	}
	got := g.FindNode("t").TestInputs
	if len(got) != 1 || got[0].ID != "s1" || got[0].Name != "Completo" || got[0].Input["cpf"] != "1" || !got[0].SavedAt.Equal(pinTime) {
		t.Fatalf("testInputs = %+v", got)
	}
	clone := g.Clone()
	clone.FindNode("t").TestInputs[0].Input["cpf"] = "2"
	if g.FindNode("t").TestInputs[0].Input["cpf"] != "1" {
		t.Fatal("a clone must not share the scenario input with the draft")
	}
}
