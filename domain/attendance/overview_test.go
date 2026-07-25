package attendance

import (
	"testing"
	"time"
)

func TestDefaultDefinitions_NotEmpty(t *testing.T) {
	d := DefaultDefinitions()
	if d.PeriodScope == "" || d.StatusMapping == "" || d.WaitTime == "" || d.HandleTime == "" {
		t.Fatalf("expected frozen metric definitions to be populated, got %+v", d)
	}
	if d.CSAT == "" || d.SLA == "" || d.Resolution == "" {
		t.Fatalf("expected CSAT/SLA/resolution docs, got %+v", d)
	}
	if d.FRT == "" || d.AI == "" || d.Queue == "" || d.Occupancy == "" {
		t.Fatalf("expected FRT/AI/queue/occupancy docs, got %+v", d)
	}
	if d.ChannelMix == "" || d.Messaging == "" || d.Reopen == "" || d.Unassigned == "" {
		t.Fatalf("expected channel/messaging/reopen/unassigned docs, got %+v", d)
	}
	if d.Engaged == "" || d.Shell == "" {
		t.Fatalf("expected engaged/shell definitions, got %+v", d)
	}
	if d.FinishedBySource == "" {
		t.Fatal("expected finished_by_source definition")
	}
}

func TestOverviewFinishedBySource_Shape(t *testing.T) {
	f := OverviewFinishedBySource{
		Human: 10, AI: 2, System: 3, Total: 15, Available: true,
	}
	if f.Human+f.AI+f.System != f.Total {
		t.Fatalf("parts must sum to total: %+v", f)
	}
}

func TestOverviewFilter_JSONTags(t *testing.T) {
	// Smoke: zero filter is valid for unscoped overview
	f := OverviewFilter{}
	if f.IncludeAI {
		t.Fatal("IncludeAI should default false in zero value (handler sets default true)")
	}
	now := time.Now().UTC()
	f.DateFrom = &now
	if f.DateFrom == nil {
		t.Fatal("DateFrom should be settable")
	}
}

func TestOverviewEmptyPayloadShape(t *testing.T) {
	out := &Overview{
		Hourly:      make([]HourlyPoint, 24),
		Definitions: DefaultDefinitions(),
		KPIs: OverviewKPIs{
			CSATAvailable: false,
			SLAAvailable:  false,
		},
	}
	for h := 0; h < 24; h++ {
		out.Hourly[h] = HourlyPoint{Hour: h}
	}
	if len(out.Hourly) != 24 {
		t.Fatalf("hourly must be 24 buckets, got %d", len(out.Hourly))
	}
	if out.KPIs.CSATAvailable || out.KPIs.SLAAvailable {
		t.Fatal("CSAT and SLA must be unavailable until product ships")
	}
	if out.KPIs.AvgRating != nil {
		t.Fatal("avg rating must be nil until CSAT")
	}
}
