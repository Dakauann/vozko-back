package workflow

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTheMeetingNodeTypesAreReadAsCalendarEventNodes(t *testing.T) {
	cases := []struct {
		stored string
		want   NodeType
	}{
		{"action_schedule_meeting", NodeTypeActionCreateCalendarEvent},
		{"action_reschedule_meeting", NodeTypeActionRescheduleCalendarEvent},
		{"action_create_calendar_event", NodeTypeActionCreateCalendarEvent},
		{"action_reschedule_calendar_event", NodeTypeActionRescheduleCalendarEvent},
	}
	for _, c := range cases {
		t.Run(c.stored, func(t *testing.T) {
			var n Node
			if err := json.Unmarshal([]byte(`{"id":"n1","type":"`+c.stored+`"}`), &n); err != nil {
				t.Fatal(err)
			}
			if n.Type != c.want {
				t.Fatalf("decoded type = %q, want %q so an import or a builder output with the old name is converted, never rejected", n.Type, c.want)
			}
			if !n.Type.Valid() {
				t.Fatalf("%q must be a valid node type", n.Type)
			}
			if got := NodeType(c.stored).Canonical(); got != c.want {
				t.Fatalf("Canonical(%q) = %q, want %q", c.stored, got, c.want)
			}
		})
	}
}

func TestACalendarEventNodeAlwaysCallsOutEvenUnderItsOldName(t *testing.T) {
	for _, nodeType := range []NodeType{
		NodeTypeActionCreateCalendarEvent, NodeTypeActionRescheduleCalendarEvent,
		NodeTypeActionScheduleMeetingLegacy, NodeTypeActionRescheduleMeetingLegacy,
	} {
		if !ExternalCallNode(Node{Type: nodeType}) {
			t.Errorf("%q must be treated as an external call", nodeType)
		}
	}
}

func TestCanonicalizingStoredGraphJSONRenamesTheLegacyNodeTypes(t *testing.T) {
	stored := `{"nodes":[{"id":"m1","type":"action_schedule_meeting","config":{"title":"Visita","create_google_meet":"false"},"pinned":{"output":{"meeting_link":""}},"position":{"x":1,"y":2}},{"id":"m2","type":"action_reschedule_meeting","config":"not a map"},{"id":"t","type":"trigger_message_received","config":{}}],"edges":[{"source":"m1","target":"m2","label":"sucesso"}]}`

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
	for i, want := range []NodeType{NodeTypeActionCreateCalendarEvent, NodeTypeActionRescheduleCalendarEvent, NodeTypeTriggerMessageReceived} {
		if got := graph.Nodes[i]["type"]; got != string(want) {
			t.Fatalf("node %d type = %v, want %q", i, got, want)
		}
	}
	if graph.Nodes[0]["pinned"] == nil || graph.Nodes[0]["config"].(map[string]interface{})["title"] != "Visita" || graph.Nodes[1]["config"] != "not a map" {
		t.Fatalf("graph = %s, only the type may change", out)
	}
	if len(graph.Edges) != 1 || graph.Edges[0]["label"] != "sucesso" {
		t.Fatalf("edges = %v", graph.Edges)
	}

	again, changedAgain, err := CanonicalizeGraphJSON(out)
	if err != nil || changedAgain || string(again) != string(out) {
		t.Fatalf("a renamed graph must come back untouched: changed = %v, err = %v", changedAgain, err)
	}
}

func TestTheLegacyNodeTypeFilterNamesEveryOldName(t *testing.T) {
	path := LegacyNodeTypeGraphPath()
	for _, nodeType := range []NodeType{NodeTypeActionScheduleMeetingLegacy, NodeTypeActionRescheduleMeetingLegacy, NodeTypeActionSendWhatsappButtonLegacy} {
		if !strings.Contains(path, `@.type == "`+string(nodeType)+`"`) {
			t.Fatalf("path %s misses %s", path, nodeType)
		}
	}
	if !strings.HasPrefix(path, "$.nodes[*] ? (") {
		t.Fatalf("path = %s", path)
	}
	if got := LegacyNodeTypes(); len(got) != 3 || got[0] > got[1] || got[1] > got[2] {
		t.Fatalf("LegacyNodeTypes() = %v, want every old name sorted", got)
	}
}
