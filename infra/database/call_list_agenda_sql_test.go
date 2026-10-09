package database

import (
	"strings"
	"testing"
)

func TestTheCallListAgendaPutsCallbacksByTimeBeforeThePlainQueue(t *testing.T) {
	if got, want := CallListAgendaSQL("i"), "COALESCE(i.callback_at, 'infinity'::timestamptz)"; got != want {
		t.Fatalf("CallListAgendaSQL = %q, want %q", got, want)
	}
}

func TestTheCallListAgendaIndexIsBuiltOnTheSameExpression(t *testing.T) {
	sql, ok := ConcurrentIndexSQL(CallListAgendaIndex)
	if !ok {
		t.Fatalf("%s is not built", CallListAgendaIndex)
	}
	for _, part := range []string{
		"CREATE INDEX CONCURRENTLY IF NOT EXISTS " + CallListAgendaIndex,
		"ON call_list_items (list_id, (" + CallListAgendaSQL("") + "), position)",
		"WHERE state = 'pending'",
	} {
		if !strings.Contains(sql, part) {
			t.Fatalf("%s lacks %q: %s", CallListAgendaIndex, part, sql)
		}
	}
}
