package workflow

import (
	"encoding/json"
	"errors"
	"testing"

	"vozko/domain/conversation"
)

func decodeJSONList(t *testing.T, raw string) []interface{} {
	t.Helper()
	var out []interface{}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func interactiveGraph(config map[string]interface{}) *Graph {
	return &Graph{Nodes: []Node{{ID: "n13", Type: NodeTypeActionSendInteractive, Config: config}}}
}

func TestInteractiveButtonsReadsTheEditorEncoding(t *testing.T) {
	buttons, err := InteractiveButtons(map[string]interface{}{
		"buttons": `[{"Type":"reply","ID":"sim","Title":"Sim"}]`,
	})

	if err != nil {
		t.Fatal(err)
	}
	if len(buttons) != 1 || buttons[0].ID != "sim" || buttons[0].Title != "Sim" {
		t.Fatalf("buttons = %+v", buttons)
	}
}

func TestInteractiveButtonsReadsAnImportedJSONArray(t *testing.T) {
	buttons, err := InteractiveButtons(map[string]interface{}{
		"buttons": decodeJSONList(t, `[{"id":"cat_musc","title":"Valores/horários"},{"id":"gympass","title":"Gympass"}]`),
	})

	if err != nil {
		t.Fatal(err)
	}
	if len(buttons) != 2 || buttons[1].ID != "gympass" || buttons[1].Title != "Gympass" {
		t.Fatalf("buttons = %+v", buttons)
	}
}

func TestInteractiveButtonsRejectsAValueThatIsNotAList(t *testing.T) {
	_, err := InteractiveButtons(map[string]interface{}{"buttons": map[string]interface{}{"id": "sim"}})

	if !errors.Is(err, ErrInteractiveOptionsNotAList) {
		t.Fatalf("err = %v, want ErrInteractiveOptionsNotAList", err)
	}
}

func TestInteractiveButtonsTreatsAMissingValueAsNoButtons(t *testing.T) {
	buttons, err := InteractiveButtons(map[string]interface{}{"buttons": nil})

	if err != nil || len(buttons) != 0 {
		t.Fatalf("buttons = %+v, err = %v", buttons, err)
	}
}

func TestInteractiveListSectionsReadsAnImportedJSONArray(t *testing.T) {
	sections, err := InteractiveListSections(map[string]interface{}{
		"sections": decodeJSONList(t, `[{"title":"Opções","rows":[{"id":"val_musc","title":"Valores","description":"Tabela de valores"}]}]`),
	})

	if err != nil {
		t.Fatal(err)
	}
	want := conversation.ListRow{ID: "val_musc", Title: "Valores", Description: "Tabela de valores"}
	if len(sections) != 1 || sections[0].Title != "Opções" || len(sections[0].Rows) != 1 || sections[0].Rows[0] != want {
		t.Fatalf("sections = %+v", sections)
	}
}

func TestInteractiveListSectionsRejectsAValueThatIsNotAList(t *testing.T) {
	_, err := InteractiveListSections(map[string]interface{}{"sections": float64(3)})

	if !errors.Is(err, ErrInteractiveOptionsNotAList) {
		t.Fatalf("err = %v, want ErrInteractiveOptionsNotAList", err)
	}
}

func TestInteractivePromptStyleDefaultsToButtons(t *testing.T) {
	if got := InteractivePromptStyle(map[string]interface{}{"interactive_type": " LIST "}); got != "list" {
		t.Errorf("style = %q, want list", got)
	}
	if got := InteractivePromptStyle(map[string]interface{}{}); got != "buttons" {
		t.Errorf("style = %q, want buttons", got)
	}
}

func TestValidateInteractivePromptConfigChecksButtonsGivenAsAnArray(t *testing.T) {
	g := interactiveGraph(map[string]interface{}{
		"interactive_type": "buttons",
		"body":             "MENU PRINCIPAL",
		"buttons":          decodeJSONList(t, `[{"id":"a","title":"A"},{"id":"b","title":"B"},{"id":"c","title":"C"},{"id":"d","title":"D"}]`),
	})

	if err := ValidateInteractivePromptConfig(g); !errors.Is(err, ErrNodeInvalidInteractiveConfig) {
		t.Fatalf("err = %v, want ErrNodeInvalidInteractiveConfig", err)
	}
}

func TestValidateInteractivePromptConfigRejectsButtonsThatAreNotAList(t *testing.T) {
	g := interactiveGraph(map[string]interface{}{"body": "Oi", "buttons": true})

	if err := ValidateInteractivePromptConfig(g); !errors.Is(err, ErrNodeInvalidInteractiveConfig) {
		t.Fatalf("err = %v, want ErrNodeInvalidInteractiveConfig", err)
	}
}

func TestValidateInteractivePromptConfigAcceptsListSectionsGivenAsAnArray(t *testing.T) {
	g := interactiveGraph(map[string]interface{}{
		"interactive_type": "list",
		"body":             "Escolha",
		"list_button":      "Escolha",
		"sections":         decodeJSONList(t, `[{"title":"Opções","rows":[{"id":"a","title":"A"}]}]`),
	})

	if err := ValidateInteractivePromptConfig(g); err != nil {
		t.Fatalf("err = %v", err)
	}
}

func TestNormalizeStoresImportedOptionsInTheEditorEncoding(t *testing.T) {
	w := &Workflow{Graph: *interactiveGraph(map[string]interface{}{
		"buttons":  decodeJSONList(t, `[{"id":"sim","title":"Sim"}]`),
		"sections": decodeJSONList(t, `[{"title":"Opções","rows":[{"id":"a","title":"A","description":"d"}]}]`),
	})}

	w.Normalize()

	config := w.Graph.Nodes[0].Config
	var buttons []map[string]interface{}
	if err := json.Unmarshal([]byte(config["buttons"].(string)), &buttons); err != nil {
		t.Fatal(err)
	}
	if len(buttons) != 1 || buttons[0]["ID"] != "sim" || buttons[0]["Title"] != "Sim" {
		t.Fatalf("buttons = %v", buttons)
	}
	var sections []map[string]interface{}
	if err := json.Unmarshal([]byte(config["sections"].(string)), &sections); err != nil {
		t.Fatal(err)
	}
	rows, _ := sections[0]["rows"].([]interface{})
	row, _ := rows[0].(map[string]interface{})
	if sections[0]["title"] != "Opções" || row["id"] != "a" || row["title"] != "A" || row["description"] != "d" {
		t.Fatalf("sections = %v", sections)
	}
}

func TestNormalizeLeavesUndecodableOptionsForValidationToReject(t *testing.T) {
	broken := []interface{}{"not a button"}
	w := &Workflow{Graph: *interactiveGraph(map[string]interface{}{"body": "Oi", "buttons": broken})}

	w.Normalize()

	if _, stillList := w.Graph.Nodes[0].Config["buttons"].([]interface{}); !stillList {
		t.Fatalf("buttons = %#v, want the original array", w.Graph.Nodes[0].Config["buttons"])
	}
	if err := ValidateInteractivePromptConfig(&w.Graph); !errors.Is(err, ErrNodeInvalidInteractiveConfig) {
		t.Fatalf("err = %v, want ErrNodeInvalidInteractiveConfig", err)
	}
}
