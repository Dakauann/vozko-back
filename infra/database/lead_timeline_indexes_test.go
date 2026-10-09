package database

import (
	"strings"
	"testing"
)

func TestTheLeadTimelineReadsDealsAndOlderCallsThroughIndexes(t *testing.T) {
	want := map[string]string{
		"idx_opportunities_workspace_lead": "ON opportunities (workspace_id, lead_id, created_at) WHERE lead_id IS NOT NULL AND deleted_at IS NULL",
		UnlinkedCallsByCounterpartIndex:    "ON calls (workspace_id, " + CallCounterpartSQL("") + ", started_at DESC) WHERE lead_id IS NULL AND deleted_at IS NULL",
	}
	for name, shape := range want {
		sql, ok := ConcurrentIndexSQL(name)
		if !ok {
			t.Fatalf("%s is not built", name)
		}
		if got := strings.Join(strings.Fields(sql), " "); !strings.Contains(got, shape) {
			t.Fatalf("%s = %q, want %q", name, got, shape)
		}
	}
	if _, ok := ConcurrentIndexSQL("idx_calls_workspace_counterpart"); ok {
		t.Fatal("the counterpart index without the start time cannot serve the timeline's newest-first read")
	}
}
