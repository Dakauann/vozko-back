package workflow

import (
	"reflect"
	"testing"
)

func TestRecordNodeOutputKeepsTheTemplateKeysTemplatesAlreadyRead(t *testing.T) {
	state := NewRunState()
	RecordNodeOutput(&state, "n55", map[string]interface{}{"status_code": 200, "body": map[string]interface{}{"accessToken": "x"}})

	cases := []struct {
		key  string
		want interface{}
	}{
		{"_last_status_code", 200},
		{"_node_n55_status_code", 200},
		{"_last_body", map[string]interface{}{"accessToken": "x"}},
		{"_node_n55_body", map[string]interface{}{"accessToken": "x"}},
		{StateKeyOutputNode, "n55"},
	}
	for _, c := range cases {
		got, ok := state.Get(c.key)
		if !ok || !reflect.DeepEqual(got, c.want) {
			t.Fatalf("%s: got %v (%v), want %v", c.key, got, ok, c.want)
		}
	}
}

func TestRecordNodeOutputDoesNotStoreTheWholeMapAgainBecauseRunStateGrowsPerStep(t *testing.T) {
	state := NewRunState()
	RecordNodeOutput(&state, "n1", map[string]interface{}{"a": 1, "b": 2})
	if len(state.Vars) != 5 {
		t.Fatalf("expected two keys per field plus the marker, got %d: %v", len(state.Vars), state.Vars)
	}
}

func TestRecordNodeOutputWithoutFieldsLeavesThePreviousProducerAsTheLastOutput(t *testing.T) {
	cases := []struct {
		name   string
		output map[string]interface{}
	}{
		{"nil output", nil},
		{"empty output", map[string]interface{}{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			state := NewRunState()
			RecordNodeOutput(&state, "http", map[string]interface{}{"status_code": 200})
			RecordNodeOutput(&state, "condition", c.output)
			got := LastNodeOutput(&state)
			if !reflect.DeepEqual(got, map[string]interface{}{"status_code": 200}) {
				t.Fatalf("got %v", got)
			}
		})
	}
}

func TestNodeOutputRebuildsOneNodesFieldsWithoutMixingNodesThatShareAPrefix(t *testing.T) {
	state := NewRunState()
	RecordNodeOutput(&state, "n5", map[string]interface{}{"ok": true})
	RecordNodeOutput(&state, "n55", map[string]interface{}{"status_code": 200})

	cases := []struct {
		nodeID string
		want   map[string]interface{}
	}{
		{"n5", map[string]interface{}{"ok": true}},
		{"n55", map[string]interface{}{"status_code": 200}},
		{"missing", map[string]interface{}{}},
		{"", map[string]interface{}{}},
	}
	for _, c := range cases {
		got := NodeOutput(&state, c.nodeID)
		if !reflect.DeepEqual(got, c.want) {
			t.Fatalf("%q: got %v, want %v", c.nodeID, got, c.want)
		}
	}
}

func TestLastNodeOutputIsOnlyTheMostRecentProducerNotStaleFieldsFromOlderNodes(t *testing.T) {
	state := NewRunState()
	RecordNodeOutput(&state, "login", map[string]interface{}{"status_code": 200, "token": "t"})
	RecordNodeOutput(&state, "check", map[string]interface{}{"status_code": 404})

	got := LastNodeOutput(&state)
	if !reflect.DeepEqual(got, map[string]interface{}{"status_code": 404}) {
		t.Fatalf("got %v", got)
	}
}

func TestLastNodeOutputFallsBackToLastKeysForStateWrittenBeforeTheMarkerExisted(t *testing.T) {
	state := NewRunState()
	state.Set("_last_status_code", 200)
	state.Set("_node_old_status_code", 200)
	state.Set("other", "ignored")

	got := LastNodeOutput(&state)
	if !reflect.DeepEqual(got, map[string]interface{}{"status_code": 200}) {
		t.Fatalf("got %v", got)
	}
}

func TestNodeOutputReadersTolerateANilState(t *testing.T) {
	if got := LastNodeOutput(nil); len(got) != 0 {
		t.Fatalf("got %v", got)
	}
	if got := NodeOutput(nil, "n1"); len(got) != 0 {
		t.Fatalf("got %v", got)
	}
	RecordNodeOutput(nil, "n1", map[string]interface{}{"a": 1})
}
