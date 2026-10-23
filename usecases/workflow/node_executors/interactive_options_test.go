package node_executors

import (
	"encoding/json"
	"errors"
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/workflow"
)

func newTestState() *workflow.RunState {
	s := workflow.NewRunState()
	return &s
}

func buttonsConfig(t *testing.T, pairs ...string) map[string]interface{} {
	t.Helper()
	type btn struct{ Type, ID, Title string }
	items := make([]btn, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		items = append(items, btn{Type: "reply", ID: pairs[i], Title: pairs[i+1]})
	}
	raw, err := json.Marshal(items)
	if err != nil {
		t.Fatal(err)
	}
	return map[string]interface{}{"interactive_type": "buttons", "buttons": string(raw)}
}

func listConfig(t *testing.T, rows ...[3]string) map[string]interface{} {
	t.Helper()
	type row struct {
		ID          string `json:"id"`
		Title       string `json:"title"`
		Description string `json:"description"`
	}
	out := make([]row, 0, len(rows))
	for _, r := range rows {
		out = append(out, row{ID: r[0], Title: r[1], Description: r[2]})
	}
	raw, err := json.Marshal([]map[string]interface{}{{"title": "", "rows": out}})
	if err != nil {
		t.Fatal(err)
	}
	return map[string]interface{}{"interactive_type": "list", "sections": string(raw)}
}

func mustOptions(t *testing.T, config map[string]interface{}, state *workflow.RunState) []conversation.InteractiveOption {
	t.Helper()
	options, err := interactiveOptionsOf(config, customerTextIn(&workflow.NodeContext{State: state}))
	if err != nil {
		t.Fatal(err)
	}
	return options
}

func importedConfig(t *testing.T, raw string) map[string]interface{} {
	t.Helper()
	var config map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		t.Fatal(err)
	}
	return config
}

func TestInteractiveOptionsReadsImportedButtonsArray(t *testing.T) {
	config := importedConfig(t, `{"interactive_type":"buttons","buttons":[{"id":"cat_musc","title":"Valores/horários"},{"id":"sobre","title":"Sobre academia"}]}`)

	got := mustOptions(t, config, newTestState())

	if len(got) != 2 || got[0].ID != "cat_musc" || got[1].Title != "Sobre academia" {
		t.Fatalf("options = %+v", got)
	}
}

func TestInteractiveOptionsReadsImportedListSectionsArray(t *testing.T) {
	config := importedConfig(t, `{"interactive_type":"list","sections":[{"title":"Opções","rows":[{"id":"val_musc","title":"Valores"}]}]}`)

	got := mustOptions(t, config, newTestState())

	if len(got) != 1 || got[0].ID != "val_musc" || got[0].Title != "Valores" {
		t.Fatalf("options = %+v", got)
	}
}

func TestInteractiveOptionsReportsButtonsThatAreNotAList(t *testing.T) {
	_, err := interactiveOptionsOf(map[string]interface{}{"buttons": float64(6)}, customerTextIn(&workflow.NodeContext{State: newTestState()}))

	if !errors.Is(err, workflow.ErrNodeInvalidInteractiveConfig) {
		t.Fatalf("err = %v, want ErrNodeInvalidInteractiveConfig", err)
	}
}

func TestAskInteractiveOutputsListsImportedButtonsAsBranches(t *testing.T) {
	config := importedConfig(t, `{"interactive_type":"buttons","buttons":[{"id":"gympass","title":"Gympass"}]}`)

	outputs := AskInteractiveOutputs(config)

	if len(outputs) == 0 || outputs[0].ID != "gympass" || outputs[0].Optional {
		t.Fatalf("outputs = %+v, want the gympass branch first", outputs)
	}
}

func TestInteractiveOptionsReadsTheButtonsShape(t *testing.T) {
	got := mustOptions(t, buttonsConfig(t, "sim", "Sim", "nao", "Não"), newTestState())

	if len(got) != 2 {
		t.Fatalf("options = %d, want 2", len(got))
	}
	if got[0].ID != "sim" || got[0].Title != "Sim" {
		t.Errorf("option = %+v", got[0])
	}
}

func TestInteractiveOptionsReadsTheListShapeAcrossSections(t *testing.T) {
	got := mustOptions(t,
		listConfig(t, [3]string{"a", "Primeira", "desc"}, [3]string{"b", "Segunda", ""}),
		newTestState(),
	)

	if len(got) != 2 {
		t.Fatalf("options = %d, want 2", len(got))
	}
	if got[0].ID != "a" || got[1].ID != "b" {
		t.Errorf("options = %+v, want the authored order preserved", got)
	}
}

func TestInteractiveOptionsInterpolatesTitlesButNeverIDs(t *testing.T) {
	state := newTestState()
	state.Set("nome", "Ana")

	got := mustOptions(t, buttonsConfig(t, "opt_{{nome}}", "Falar com {{nome}}"), state)

	if len(got) != 1 {
		t.Fatalf("options = %d", len(got))
	}
	if got[0].Title != "Falar com Ana" {
		t.Errorf("title = %q, want the interpolated label", got[0].Title)
	}
	if got[0].ID != "opt_{{nome}}" {
		t.Errorf("id = %q, interpolating a routing key breaks the branch match", got[0].ID)
	}
}

func TestInteractiveOptionsSkipsRowsWithNoID(t *testing.T) {
	got := mustOptions(t,
		listConfig(t, [3]string{"", "Sem id", ""}, [3]string{"b", "Com id", ""}),
		newTestState(),
	)
	if len(got) != 1 || got[0].ID != "b" {
		t.Errorf("options = %+v, want only the routable row", got)
	}
}

func TestInteractiveOptionsIsEmptyForAnUnconfiguredNode(t *testing.T) {
	if got := mustOptions(t, map[string]interface{}{}, newTestState()); len(got) != 0 {
		t.Errorf("options = %+v, want none", got)
	}
}
