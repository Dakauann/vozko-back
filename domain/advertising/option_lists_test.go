package advertising

import (
	"slices"
	"testing"
)

func TestPixelEventsListEveryValidEventAndIsACopy(t *testing.T) {
	events := PixelEvents()
	if len(events) != 8 {
		t.Fatalf("got %d events", len(events))
	}
	for _, e := range events {
		if !e.Valid() {
			t.Fatalf("%s listed but not valid", e)
		}
	}
	if PixelEvent("VIEW_CONTENT").Valid() {
		t.Fatal("unknown event accepted")
	}
	events[0] = "CHANGED"
	if PixelEvents()[0] == "CHANGED" {
		t.Fatal("the list is shared with callers")
	}
}

func TestRuleMetricsListEveryValidMetricAndIsACopy(t *testing.T) {
	metrics := RuleMetrics()
	if len(metrics) != 9 {
		t.Fatalf("got %d metrics", len(metrics))
	}
	for _, m := range metrics {
		if !m.Valid() {
			t.Fatalf("%s listed but not valid", m)
		}
	}
	if RuleMetric("roas").Valid() {
		t.Fatal("unknown metric accepted")
	}
	metrics[0] = "changed"
	if RuleMetrics()[0] == "changed" {
		t.Fatal("the list is shared with callers")
	}
}

func TestCreativeFormatsCoverEveryDestinationFormat(t *testing.T) {
	formats := CreativeFormats()
	destinations := append([]Destination{DestinationWebsite, DestinationInstantForm, DestinationApp, DestinationOnPost, DestinationNone, DestinationCatalog}, messagingDestinations...)
	for _, d := range destinations {
		for _, f := range formatsFor(d) {
			if !slices.Contains(formats, f) {
				t.Fatalf("%s allowed for %s but not listed", f, d)
			}
		}
	}
	formats[0] = "CHANGED"
	if CreativeFormats()[0] == "CHANGED" {
		t.Fatal("the list is shared with callers")
	}
}
