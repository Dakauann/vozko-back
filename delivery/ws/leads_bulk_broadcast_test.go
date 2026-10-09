package ws

import (
	"encoding/json"
	"testing"
)

func TestABulkLeadRunReachesLeadReadersOnlyWithItsRunID(t *testing.T) {
	state := &capturingSharedState{}
	hub := leadHub(t, map[string]bool{"u-reader": true, "u-both": true}, state)

	NewLeadChangeNotifier(hub, leadEntries{}).LeadsBulkUpdated("ws-1", "run-1")

	for id, want := range map[string]int{"c-reader": 1, "c-both": 1, "c-subscriber": 0, "c-bystander": 0, "c-foreign": 0} {
		got := drained(hub.connections[id])
		if len(got) != want {
			t.Fatalf("%s received %d messages, want %d", id, len(got), want)
		}
		if want == 0 {
			continue
		}
		var raw map[string]any
		_ = json.Unmarshal([]byte(got[0]), &raw)
		payload, _ := raw["payload"].(map[string]any)
		if raw["type"] != "conversation:leads_bulk_update" || payload["runId"] != "run-1" || len(payload) != 1 {
			t.Fatalf("message = %s", got[0])
		}
	}
	if len(state.published) != 1 {
		t.Fatalf("the run must be published to the other replicas once, got %d", len(state.published))
	}
	var envelope redisWorkspaceBroadcast
	if err := json.Unmarshal(state.published[0], &envelope); err != nil {
		t.Fatal(err)
	}
	other := leadHub(t, map[string]bool{"u-reader": true}, &capturingSharedState{})
	other.deliverWorkspaceBroadcast(envelope)
	if len(drained(other.connections["c-reader"])) != 1 || len(drained(other.connections["c-subscriber"])) != 0 {
		t.Fatal("the replica must deliver the run to lead readers only")
	}
}
