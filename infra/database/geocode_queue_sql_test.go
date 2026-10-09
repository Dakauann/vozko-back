package database

import (
	"strings"
	"testing"
)

func TestGeocodeQueuedSQLListsEveryQueuedStatus(t *testing.T) {
	if got, want := GeocodeQueuedSQL(""), "geo_status IN ('pending', 'unavailable', 'quota_exceeded')"; got != want {
		t.Fatalf("GeocodeQueuedSQL() = %q, want %q", got, want)
	}
	if got, want := GeocodeQueuedSQL("a"), "a.geo_status IN ('pending', 'unavailable', 'quota_exceeded')"; got != want {
		t.Fatalf("GeocodeQueuedSQL(a) = %q, want %q", got, want)
	}
}

func TestTheGeocodeQueueIndexServesTheRoundRobinClaimAndTheBacklogCount(t *testing.T) {
	idx := namedIndex(t, GeocodeQueueIndex)
	sql := strings.Join(strings.Fields(idx.sql), " ")
	want := "CREATE INDEX CONCURRENTLY IF NOT EXISTS " + GeocodeQueueIndex + " ON lead_addresses (workspace_id, geo_next_at) INCLUDE (geo_status) WHERE " + GeocodeQueuedSQL("")
	if sql != want {
		t.Fatalf("queue index = %q, want %q", sql, want)
	}
	if idx.replaces != "idx_lead_addresses_geo_queue" {
		t.Fatalf("queue index replaces %q, want the status-first queue index", idx.replaces)
	}
}
