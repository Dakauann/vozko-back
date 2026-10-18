package sqlexpr

import "testing"

func TestGeocodeQueuedSQLListsEveryQueuedStatus(t *testing.T) {
	if got, want := GeocodeQueued(""), "geo_status IN ('pending', 'unavailable')"; got != want {
		t.Fatalf("GeocodeQueued() = %q, want %q", got, want)
	}
	if got, want := GeocodeQueued("a"), "a.geo_status IN ('pending', 'unavailable')"; got != want {
		t.Fatalf("GeocodeQueued(a) = %q, want %q", got, want)
	}
}
