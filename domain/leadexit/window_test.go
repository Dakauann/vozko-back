package leadexit

import (
	"testing"
	"time"
)

func TestTheConfirmationGoesOutOnlyInsideTheOpenWindow(t *testing.T) {
	received := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		now  time.Time
		want bool
	}{
		{"right away", received.Add(time.Second), true},
		{"almost a day later", received.Add(23*time.Hour + 59*time.Minute), true},
		{"a day later", received.Add(24 * time.Hour), false},
		{"unknown time", time.Time{}, false},
	}
	for _, tc := range cases {
		if got := WindowOpen(received, tc.now); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
	if WindowOpen(time.Time{}, received) {
		t.Fatal("a message without a time opened the window")
	}
}
