package sqlexpr

import "testing"

func TestTheCallListAgendaPutsCallbacksByTimeBeforeThePlainQueue(t *testing.T) {
	if got, want := CallListAgenda("i"), "COALESCE(i.callback_at, 'infinity'::timestamptz)"; got != want {
		t.Fatalf("CallListAgenda = %q, want %q", got, want)
	}
}
