package attendance

import "testing"

func TestBuildRankedDimensionFoldsTheTail(t *testing.T) {
	tallies := []XrayTally{
		{Key: "a", Label: "Web", Count: 1474},
		{Key: "b", Label: "Maria", Count: 238},
		{Key: "c", Label: "Rafael", Count: 209},
		{Key: "d", Count: 50},
		{Key: "e", Count: 40},
		{Key: "f", Count: 30},
		{Key: "g", Count: 20},
	}
	got := BuildRankedDimension(XrayDimensionOrigin, tallies, 0, 3)

	if !got.Available {
		t.Fatalf("BuildRankedDimension() Available = false, want true")
	}
	if len(got.Buckets) != 4 {
		t.Fatalf("BuildRankedDimension() produced %d buckets, want 3 plus the tail", len(got.Buckets))
	}
	tail := got.Buckets[3]
	if tail.Key != XrayOtherKey || tail.Count != 140 {
		t.Fatalf("BuildRankedDimension() tail = %+v, want %q at 140", tail, XrayOtherKey)
	}
	if got.Measured != 2061 {
		t.Fatalf("BuildRankedDimension() Measured = %d, want 2061", got.Measured)
	}
}

func TestBuildRankedDimensionPercentagesAreOverMeasured(t *testing.T) {
	tallies := []XrayTally{
		{Key: "a", Count: 25},
		{Key: "b", Count: 75},
	}
	got := BuildRankedDimension(XrayDimensionAssignee, tallies, 900, 5)

	if got.Measured != 100 {
		t.Fatalf("BuildRankedDimension() Measured = %d, want 100", got.Measured)
	}
	if got.Unknown != 900 {
		t.Fatalf("BuildRankedDimension() Unknown = %d, want 900", got.Unknown)
	}
	for _, bucket := range got.Buckets {
		if bucket.Key == "b" && bucket.Pct != 75 {
			t.Fatalf("BuildRankedDimension() pct = %v, want 75 over the measured population", bucket.Pct)
		}
	}
}

func TestBuildRankedDimensionWithNoCoverage(t *testing.T) {
	got := BuildRankedDimension(XrayDimensionTenure, nil, 500, 5)

	if got.Available {
		t.Fatalf("BuildRankedDimension() with no coverage Available = true, want false")
	}
	if got.Reason != ReasonNoCoverage {
		t.Fatalf("BuildRankedDimension() Reason = %q, want %q", got.Reason, ReasonNoCoverage)
	}
	if len(got.Buckets) != 0 {
		t.Fatalf("BuildRankedDimension() produced buckets with no coverage")
	}
}

func TestBuildOrderedDimensionKeepsBandOrder(t *testing.T) {
	tallies := []XrayTally{
		{Key: "30d_plus", Count: 10, Position: 4},
		{Key: "0_1d", Count: 100, Position: 0},
		{Key: "7_30d", Count: 20, Position: 3},
		{Key: "1_3d", Count: 50, Position: 1},
		{Key: "3_7d", Count: 30, Position: 2},
	}
	got := BuildOrderedDimension(XrayDimensionAge, tallies, 0)

	want := []string{"0_1d", "1_3d", "3_7d", "7_30d", "30d_plus"}
	if len(got.Buckets) != len(want) {
		t.Fatalf("BuildOrderedDimension() produced %d buckets, want %d", len(got.Buckets), len(want))
	}
	for i, key := range want {
		if got.Buckets[i].Key != key {
			t.Fatalf("BuildOrderedDimension() bucket %d = %q, want %q", i, got.Buckets[i].Key, key)
		}
	}
}

func TestBuildRecordCompleteness(t *testing.T) {
	got := BuildRecordCompleteness(
		[]RecordFieldTally{{Key: "name", Filled: 80}, {Key: "age", Filled: 20}},
		100,
		20,
	)

	if !got.Available {
		t.Fatalf("BuildRecordCompleteness() Available = false, want true")
	}
	if got.AvgFillPct == nil || *got.AvgFillPct != 50 {
		t.Fatalf("BuildRecordCompleteness() AvgFillPct = %v, want 50", got.AvgFillPct)
	}
	if len(got.Fields) != 2 || got.Fields[0].Pct != 80 {
		t.Fatalf("BuildRecordCompleteness() fields = %+v, want name at 80%%", got.Fields)
	}
}

func TestBuildRecordCompletenessWithoutFieldsOrBacklog(t *testing.T) {
	noFields := BuildRecordCompleteness(nil, 100, 0)
	if noFields.Available || noFields.Reason != ReasonNoCoverage {
		t.Fatalf("BuildRecordCompleteness() with no fields = %+v, want unavailable and %q", noFields, ReasonNoCoverage)
	}

	noBacklog := BuildRecordCompleteness([]RecordFieldTally{{Key: "name"}}, 0, 0)
	if noBacklog.Available || noBacklog.Reason != ReasonNoBacklog {
		t.Fatalf("BuildRecordCompleteness() with no backlog = %+v, want unavailable and %q", noBacklog, ReasonNoBacklog)
	}
}

func TestBuildReachabilityPerChannel(t *testing.T) {
	supported := BuildReachability("whatsapp", 200, 60, true)
	if !supported.Available {
		t.Fatalf("BuildReachability() Available = false, want true")
	}
	if supported.WindowClosed != 140 || supported.ClosedPct != 70 {
		t.Fatalf("BuildReachability() = %+v, want 140 closed at 70%%", supported)
	}

	unsupported := BuildReachability("telegram", 90, 0, false)
	if unsupported.Available {
		t.Fatalf("BuildReachability() on a channel with no window model Available = true, want false")
	}
	if unsupported.Reason != ReasonNoWindow {
		t.Fatalf("BuildReachability() Reason = %q, want %q", unsupported.Reason, ReasonNoWindow)
	}
	if unsupported.ClosedPct != 0 || unsupported.WindowClosed != 0 {
		t.Fatalf("BuildReachability() on an unsupported channel invented %+v", unsupported)
	}
	if unsupported.Measured != 90 {
		t.Fatalf("BuildReachability() Measured = %d, want the population stated even when unsupported", unsupported.Measured)
	}
}

func TestBuildReachabilityClampsAnImpossibleOpenCount(t *testing.T) {
	got := BuildReachability("whatsapp", 10, 99, true)
	if got.WindowOpen != 10 || got.WindowClosed != 0 {
		t.Fatalf("BuildReachability() = %+v, want the open count clamped to the measured population", got)
	}
}
