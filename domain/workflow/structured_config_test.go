package workflow

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

const realCapitalizedListSections = `[{"Title":"Aulas Aquáticas","Rows":[{"ID":"1","Title":"Horários Aquáticas 🏊‍♀️"},{"ID":"2","Title":"Materiais Aquáticos 🥽"}]}]`

const canonicalListSections = `[{"title":"Aulas Aquáticas","rows":[{"id":"1","title":"Horários Aquáticas 🏊‍♀️","description":""},{"id":"2","title":"Materiais Aquáticos 🥽","description":""}]}]`

const canonicalButtons = `[{"Type":"reply","ID":"sim","Title":"Sim & <ok>","CopyCode":""}]`

func canonicalizedNode(t *testing.T, nodeType NodeType, config map[string]interface{}) map[string]interface{} {
	t.Helper()
	g := &Graph{Nodes: []Node{{ID: "n27", Type: nodeType, Config: config}}}
	CanonicalizeStructuredConfigs(g)
	return g.Nodes[0].Config
}

func TestEveryKeyCaseOfListSectionsBecomesTheOneCanonicalShape(t *testing.T) {
	for name, sections := range map[string]interface{}{
		"capitalized, as the builder wrote it": realCapitalizedListSections,
		"mixed case":                           `[{"TITLE":"Aulas Aquáticas","rOwS":[{"Id":"1","title":"Horários Aquáticas 🏊‍♀️"},{"id":"2","TiTlE":"Materiais Aquáticos 🥽","Description":""}]}]`,
		"already canonical":                    canonicalListSections,
		"an imported array":                    decodeJSONList(t, realCapitalizedListSections),
	} {
		t.Run(name, func(t *testing.T) {
			config := canonicalizedNode(t, NodeTypeActionSendInteractive, map[string]interface{}{"interactive_type": "list", "sections": sections})

			if got := config["sections"]; got != canonicalListSections {
				t.Fatalf("sections = %v\nwant      %s", got, canonicalListSections)
			}
		})
	}
}

func TestEveryKeyCaseOfButtonsBecomesTheEditorEncoding(t *testing.T) {
	for name, buttons := range map[string]interface{}{
		"lowercase":         `[{"type":"reply","id":"sim","title":"Sim & <ok>"}]`,
		"mixed case":        `[{"TYPE":"reply","Id":"sim","tItLe":"Sim & <ok>","copycode":""}]`,
		"already canonical": canonicalButtons,
		"an imported array": decodeJSONList(t, `[{"type":"reply","id":"sim","title":"Sim & <ok>"}]`),
	} {
		t.Run(name, func(t *testing.T) {
			config := canonicalizedNode(t, NodeTypeActionSendInteractive, map[string]interface{}{"buttons": buttons})

			if got := config["buttons"]; got != canonicalButtons {
				t.Fatalf("buttons = %v\nwant      %s", got, canonicalButtons)
			}
		})
	}
}

func TestCanonicalizingTwiceChangesNothing(t *testing.T) {
	g := &Graph{Nodes: []Node{{ID: "n27", Type: NodeTypeActionSendInteractive, Config: map[string]interface{}{"sections": realCapitalizedListSections}}}}

	if !CanonicalizeStructuredConfigs(g) {
		t.Fatal("the first pass must report a change")
	}
	if CanonicalizeStructuredConfigs(g) {
		t.Fatal("a canonical graph must report no change")
	}
}

func TestCanonicalizingLeavesAnUnreadableValueForLintToReport(t *testing.T) {
	for name, value := range map[string]interface{}{
		"broken json":         "{not json",
		"an object, no list":  map[string]interface{}{"id": "sim"},
		"a list of strings":   decodeJSONList(t, `["not a section"]`),
		"a number":            float64(3),
		"rows that are words": `[{"title":"x","rows":"nope"}]`,
	} {
		t.Run(name, func(t *testing.T) {
			g := &Graph{Nodes: []Node{{ID: "n27", Type: NodeTypeActionSendInteractive, Config: map[string]interface{}{"sections": value}}}}

			if CanonicalizeStructuredConfigs(g) {
				t.Fatal("an unreadable value must be left as it is")
			}
			problems := StructuredConfigProblems(g)
			if len(problems) != 1 || !errors.Is(problems[0], ErrNodeUnreadableConfig) {
				t.Fatalf("problems = %v, want one ErrNodeUnreadableConfig", problems)
			}
			if nodeIDFromErr(problems[0]) != "n27" || fieldFromErr(problems[0]) != "sections" {
				t.Fatalf("problem %q must name the node and the field", problems[0])
			}
		})
	}
}

func TestEmptyStructuredValuesAreNotProblems(t *testing.T) {
	g := &Graph{Nodes: []Node{{ID: "n27", Type: NodeTypeActionSendInteractive, Config: map[string]interface{}{"buttons": "", "sections": "[]"}}}}

	CanonicalizeStructuredConfigs(g)

	if problems := StructuredConfigProblems(g); len(problems) != 0 {
		t.Fatalf("problems = %v", problems)
	}
	if g.Nodes[0].Config["buttons"] != "" || g.Nodes[0].Config["sections"] != "[]" {
		t.Fatalf("config = %v", g.Nodes[0].Config)
	}
}

func TestNormalizeCanonicalizesTheOptionsTheBuilderWroteWithCapitalizedKeys(t *testing.T) {
	w := &Workflow{Graph: *interactiveGraph(map[string]interface{}{"interactive_type": "list", "sections": realCapitalizedListSections})}

	w.Normalize()

	if got := w.Graph.Nodes[0].Config["sections"]; got != canonicalListSections {
		t.Fatalf("sections = %v", got)
	}
}

func TestLintReportsAnUnreadableStructuredConfigAsBlocking(t *testing.T) {
	g := interactiveGraph(map[string]interface{}{"interactive_type": "list", "body": "Oi", "buttons": "{not json"})

	report := LintGraph(g, WorkflowTypeMessages, nil, nil, nil)

	for _, issue := range report.Blocking() {
		if issue.Code == LintConfigShapeMismatch && issue.NodeID == "n13" && issue.Field == "buttons" {
			return
		}
	}
	t.Fatalf("issues = %+v, want a blocking %s on n13.buttons", report.Issues, LintConfigShapeMismatch)
}

type interactiveHandleCase struct {
	Name    string                 `json:"name"`
	Config  map[string]interface{} `json:"config"`
	Handles []string               `json:"handles"`
}

func interactiveHandleCases(t *testing.T) []interactiveHandleCase {
	t.Helper()
	raw, err := os.ReadFile("testdata/interactive_handles.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []interactiveHandleCase
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	return cases
}

func TestInteractiveOptionHandlesAreTheServerRuleTheEditorDrawsFrom(t *testing.T) {
	for _, c := range interactiveHandleCases(t) {
		t.Run(c.Name, func(t *testing.T) {
			handles := InteractiveOptionHandles(c.Config)

			ids := make([]string, 0, len(handles))
			for _, h := range handles {
				ids = append(ids, h.ID)
			}
			if len(ids) != len(c.Handles) {
				t.Fatalf("handles = %v, want %v", ids, c.Handles)
			}
			for i := range ids {
				if ids[i] != c.Handles[i] {
					t.Fatalf("handles = %v, want %v", ids, c.Handles)
				}
			}
		})
	}
}

func TestCanonicalizingStoredGraphJSONTouchesOnlyStructuredConfigs(t *testing.T) {
	stored := `{"nodes":[{"id":"n27","type":"action_send_interactive","config":{"interactive_type":"list","sections":` + mustQuote(t, realCapitalizedListSections) + `,"timeout_seconds":300},"position":{"x":5096,"y":1186},"extra":{"kept":true}},{"id":"old","type":"action_send_whatsapp_button","config":{"buttons":"[{\"id\":\"a\",\"title\":\"A\"}]"}},{"id":"n44","type":"wait_duration","config":{"seconds":1}}],"edges":[{"label":"1","source":"n27","target":"n44"}]}`

	out, changed, err := CanonicalizeGraphJSON([]byte(stored))
	if err != nil || !changed {
		t.Fatalf("changed = %v, err = %v", changed, err)
	}
	var graph struct {
		Nodes []map[string]interface{} `json:"nodes"`
		Edges []map[string]interface{} `json:"edges"`
	}
	if err := json.Unmarshal(out, &graph); err != nil {
		t.Fatal(err)
	}
	config := graph.Nodes[0]["config"].(map[string]interface{})
	if config["sections"] != canonicalListSections || config["timeout_seconds"] != float64(300) {
		t.Fatalf("config = %v", config)
	}
	if graph.Nodes[0]["extra"] == nil || len(graph.Edges) != 1 || graph.Edges[0]["label"] != "1" {
		t.Fatalf("graph = %s, the rest of the graph must survive", out)
	}
	if got := graph.Nodes[1]["config"].(map[string]interface{})["buttons"]; got != `[{"Type":"","ID":"a","Title":"A","CopyCode":""}]` {
		t.Fatalf("legacy node buttons = %v", got)
	}

	again, changedAgain, err := CanonicalizeGraphJSON(out)
	if err != nil || changedAgain || string(again) != string(out) {
		t.Fatalf("a canonical graph must come back untouched: changed = %v, err = %v", changedAgain, err)
	}
}

func TestStoredGraphJSONThatCannotBeReadIsAnError(t *testing.T) {
	if _, _, err := CanonicalizeGraphJSON([]byte("{not json")); err == nil {
		t.Fatal("want an error")
	}
}

func TestTheStoredGraphFilterNamesEveryNodeTypeWithStructuredConfig(t *testing.T) {
	path := StructuredConfigGraphPath()

	for _, nodeType := range []NodeType{NodeTypeActionSendInteractive, NodeTypeActionSendWhatsappButtonLegacy} {
		if !strings.Contains(path, `@.type == "`+string(nodeType)+`"`) {
			t.Fatalf("path %s misses %s", path, nodeType)
		}
	}
	if !strings.HasPrefix(path, "$.nodes[*] ? (") {
		t.Fatalf("path = %s", path)
	}
}

func mustQuote(t *testing.T, s string) string {
	t.Helper()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
