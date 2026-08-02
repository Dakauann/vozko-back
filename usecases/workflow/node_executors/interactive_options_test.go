package node_executors

import (
	"encoding/json"
	"testing"

	"vozko/domain/workflow"
)

// NodeContext carries a *RunState, while NewRunState returns a value.
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

func TestInteractiveOptionsReadsTheButtonsShape(t *testing.T) {
	got := interactiveOptionsOf(buttonsConfig(t, "sim", "Sim", "nao", "Não"), newTestState())

	if len(got) != 2 {
		t.Fatalf("options = %d, want 2", len(got))
	}
	if got[0].ID != "sim" || got[0].Title != "Sim" {
		t.Errorf("option = %+v", got[0])
	}
}

func TestInteractiveOptionsReadsTheListShapeAcrossSections(t *testing.T) {
	got := interactiveOptionsOf(
		listConfig(t, [3]string{"a", "Primeira", "desc"}, [3]string{"b", "Segunda", ""}),
		newTestState(),
	)

	if len(got) != 2 {
		t.Fatalf("options = %d, want 2", len(got))
	}
	// Order is the author's order, because it is also the order the options are
	// rendered in and therefore which ones fall past a channel's cap.
	if got[0].ID != "a" || got[1].ID != "b" {
		t.Errorf("options = %+v, want the authored order preserved", got)
	}
}

// Titles are shown to the contact and may reference run state; ids are routing
// keys that must match the branch label byte-for-byte, so they are never
// interpolated.
func TestInteractiveOptionsInterpolatesTitlesButNeverIDs(t *testing.T) {
	state := newTestState()
	state.Set("nome", "Ana")

	got := interactiveOptionsOf(buttonsConfig(t, "opt_{{nome}}", "Falar com {{nome}}"), state)

	if len(got) != 1 {
		t.Fatalf("options = %d", len(got))
	}
	if got[0].Title != "Falar com Ana" {
		t.Errorf("title = %q, want the interpolated label", got[0].Title)
	}
	if got[0].ID != "opt_{{nome}}" {
		t.Errorf("id = %q — interpolating a routing key breaks the branch match", got[0].ID)
	}
}

func TestInteractiveOptionsSkipsRowsWithNoID(t *testing.T) {
	got := interactiveOptionsOf(
		listConfig(t, [3]string{"", "Sem id", ""}, [3]string{"b", "Com id", ""}),
		newTestState(),
	)
	if len(got) != 1 || got[0].ID != "b" {
		t.Errorf("options = %+v, want only the routable row", got)
	}
}

func TestInteractiveOptionsIsEmptyForAnUnconfiguredNode(t *testing.T) {
	if got := interactiveOptionsOf(map[string]interface{}{}, newTestState()); len(got) != 0 {
		t.Errorf("options = %+v, want none", got)
	}
}
