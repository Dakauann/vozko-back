package advertising

import (
	"testing"
	"time"
)

func TestHistoryUsesMetasOwnWordingInTheViewersLanguage(t *testing.T) {
	cases := map[string]string{"pt": "pt_BR", "en-US": "en_US", "es": "es_LA", "de": "de_DE", "": "pt_BR", "fr": "pt_BR"}
	for in, want := range cases {
		if got := MetaLocale(in); got != want {
			t.Fatalf("%q: %q", in, got)
		}
	}
}

func TestHistoryShowsTheNewestChangesFirstUpToTheCap(t *testing.T) {
	base := time.Date(2026, 10, 3, 23, 0, 0, 0, time.UTC)
	many := make([]AdActivity, MaxActivities+5)
	for i := range many {
		many[i] = AdActivity{EventType: "update", At: base.Add(time.Duration(i) * time.Minute)}
	}
	got := NewestActivities(many)
	if len(got) != MaxActivities || !got[0].At.Equal(base.Add(time.Duration(MaxActivities+4)*time.Minute)) {
		t.Fatalf("first %v len %d", got[0].At, len(got))
	}
}
