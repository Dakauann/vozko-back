package carriercampaign

import (
	"testing"
	"time"

	"vozko/domain/carriermessaging"
)

func TestAttributes(t *testing.T) {
	delivered := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		won  time.Time
		days int
		want bool
	}{
		{"won a day later", delivered.Add(24 * time.Hour), 7, true},
		{"won at the window edge", delivered.Add(7 * 24 * time.Hour), 7, true},
		{"won just past the window", delivered.Add(7*24*time.Hour + time.Second), 7, false},
		{"won at the delivery instant", delivered, 7, false},
		{"won before delivery", delivered.Add(-time.Hour), 7, false},
		{"zero day window", delivered.Add(time.Minute), 0, false},
		{"window above the maximum", delivered.Add(time.Minute), carriermessaging.MaxAttributionDays + 1, false},
		{"maximum window", delivered.Add(time.Duration(carriermessaging.MaxAttributionDays) * 24 * time.Hour), carriermessaging.MaxAttributionDays, true},
		{"won an hour later", delivered.Add(time.Hour), 7, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Attributes(delivered, tc.won, tc.days); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
	if Attributes(time.Time{}, delivered, 7) {
		t.Fatal("a message never delivered cannot convert")
	}
}

func TestLastTouch(t *testing.T) {
	base := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	touches := []Touch{
		{MessageID: "m1", CampaignID: "c1", RunID: "r1", Variant: 0, DeliveredAt: base},
		{MessageID: "m3", CampaignID: "c2", RunID: "r3", Variant: 1, DeliveredAt: base.Add(48 * time.Hour)},
		{MessageID: "m2", CampaignID: "c1", RunID: "r2", Variant: 0, DeliveredAt: base.Add(24 * time.Hour)},
		{MessageID: "m4", CampaignID: "c3", RunID: "r4", DeliveredAt: base.Add(96 * time.Hour)},
		{MessageID: "m0", CampaignID: "c4", RunID: "r0"},
	}
	cases := []struct {
		name string
		won  time.Time
		days int
		want string
		ok   bool
	}{
		{"latest before the win", base.Add(72 * time.Hour), 7, "m3", true},
		{"after the last touch", base.Add(100 * time.Hour), 7, "m4", true},
		{"older touches outside the window", base.Add(49*time.Hour + 2*24*time.Hour), 2, "m4", true},
		{"no touch before the win", base.Add(-time.Hour), 7, "", false},
		{"every touch too old", base.Add(30 * 24 * time.Hour), 7, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := LastTouch(touches, tc.won, tc.days)
			if ok != tc.ok || got.MessageID != tc.want {
				t.Fatalf("got %q %v, want %q %v", got.MessageID, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestLastTouchTieIsStable(t *testing.T) {
	at := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	won := at.Add(time.Hour)
	a := []Touch{{MessageID: "a", DeliveredAt: at}, {MessageID: "b", DeliveredAt: at}}
	b := []Touch{{MessageID: "b", DeliveredAt: at}, {MessageID: "a", DeliveredAt: at}}
	first, _ := LastTouch(a, won, 7)
	second, _ := LastTouch(b, won, 7)
	if first.MessageID != second.MessageID || first.MessageID != "b" {
		t.Fatalf("tie picked %q and %q", first.MessageID, second.MessageID)
	}
}
