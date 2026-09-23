package attendance

import "testing"

func closedPoint(bucket string, value float64) TrendPoint {
	return TrendPoint{Bucket: bucket, Value: value}
}

func TestBuildTrendBestClosedBucketHigherIsBetter(t *testing.T) {
	spec, _ := Metric(MetricFinished)
	points := []TrendPoint{
		closedPoint("2026-06", 1702),
		closedPoint("2026-07", 1895),
		closedPoint("2026-08", 1729),
		{Bucket: "2026-09", Value: 1538, Partial: true},
	}

	got := BuildTrend(spec, points, ptr(1699))
	if !got.Available {
		t.Fatalf("BuildTrend() Available = false, want true")
	}
	if got.BestBucket != "2026-07" {
		t.Fatalf("BuildTrend() BestBucket = %q, want 2026-07", got.BestBucket)
	}
	if got.BestValue == nil || *got.BestValue != 1895 {
		t.Fatalf("BuildTrend() BestValue = %v, want 1895", got.BestValue)
	}
	if got.WindowFrom != "2026-06" || got.WindowTo != "2026-09" {
		t.Fatalf("BuildTrend() window = %q..%q, want 2026-06..2026-09", got.WindowFrom, got.WindowTo)
	}
}

func TestBuildTrendNeverCrownsThePartialBucket(t *testing.T) {
	spec, _ := Metric(MetricFinished)
	points := []TrendPoint{
		closedPoint("2026-07", 100),
		{Bucket: "2026-08", Value: 9999, Partial: true},
	}

	got := BuildTrend(spec, points, nil)
	if got.BestBucket != "2026-07" {
		t.Fatalf("BuildTrend() BestBucket = %q, want the best CLOSED bucket 2026-07", got.BestBucket)
	}
}

func TestBuildTrendLowerIsBetterPicksTheSmallest(t *testing.T) {
	spec := MetricSpec{Key: "pending_stock", Kind: MetricKindCount, Direction: DirectionLowerIsBetter}
	points := []TrendPoint{
		closedPoint("2026-06", 7124),
		closedPoint("2026-07", 5374),
		closedPoint("2026-08", 6751),
	}

	got := BuildTrend(spec, points, nil)
	if got.BestBucket != "2026-07" || got.BestValue == nil || *got.BestValue != 5374 {
		t.Fatalf("BuildTrend() best = %q/%v, want 2026-07/5374", got.BestBucket, got.BestValue)
	}
}

func TestBuildTrendAppendsTheProjectedPoint(t *testing.T) {
	spec, _ := Metric(MetricFinished)
	points := []TrendPoint{
		closedPoint("2026-07", 1000),
		{Bucket: "2026-08", Value: 800, Partial: true},
	}

	got := BuildTrend(spec, points, ptr(1200))
	if len(got.Points) != 3 {
		t.Fatalf("BuildTrend() produced %d points, want 3", len(got.Points))
	}
	last := got.Points[2]
	if !last.Projected || last.Bucket != "2026-08" || last.Value != 1200 {
		t.Fatalf("BuildTrend() last point = %+v, want a projected 2026-08 at 1200", last)
	}
}

func TestBuildTrendDeltaAgainstThePreviousClosedBucket(t *testing.T) {
	spec, _ := Metric(MetricFinished)
	points := []TrendPoint{
		closedPoint("2026-07", 1000),
		{Bucket: "2026-08", Value: 500, Partial: true},
	}

	got := BuildTrend(spec, points, ptr(1100))
	if got.PrevClosed == nil || *got.PrevClosed != 1000 {
		t.Fatalf("BuildTrend() PrevClosed = %v, want 1000", got.PrevClosed)
	}
	if got.DeltaPct == nil || *got.DeltaPct != 10 {
		t.Fatalf("BuildTrend() DeltaPct = %v, want 10", got.DeltaPct)
	}
}

func TestBuildTrendZeroPreviousBucketYieldsNoDelta(t *testing.T) {
	spec, _ := Metric(MetricFinished)
	points := []TrendPoint{
		closedPoint("2026-07", 0),
		{Bucket: "2026-08", Value: 500, Partial: true},
	}

	got := BuildTrend(spec, points, nil)
	if got.DeltaPct != nil {
		t.Fatalf("BuildTrend() DeltaPct = %v, want nil against a zero previous bucket", *got.DeltaPct)
	}
}

func TestBuildTrendWithNoClosedBucketIsUnavailable(t *testing.T) {
	spec, _ := Metric(MetricFinished)
	points := []TrendPoint{{Bucket: "2026-09", Value: 12, Partial: true}}

	got := BuildTrend(spec, points, nil)
	if got.Available {
		t.Fatalf("BuildTrend() with only a partial bucket Available = true, want false")
	}
	if got.Reason != ReasonNoClosedBuckets {
		t.Fatalf("BuildTrend() Reason = %q, want %q", got.Reason, ReasonNoClosedBuckets)
	}
	if got.BestValue != nil {
		t.Fatalf("BuildTrend() BestValue = %v, want nil", *got.BestValue)
	}
}

func TestBuildTrendKeepsAHoleInTheSeries(t *testing.T) {
	spec, _ := Metric(MetricFinished)
	points := []TrendPoint{
		closedPoint("2026-06", 100),
		closedPoint("2026-07", 0),
		closedPoint("2026-08", 200),
	}

	got := BuildTrend(spec, points, nil)
	if len(got.Points) != 3 {
		t.Fatalf("BuildTrend() dropped a bucket: %d points, want 3", len(got.Points))
	}
	if got.Points[1].Value != 0 {
		t.Fatalf("BuildTrend() rewrote the empty bucket to %v, want 0", got.Points[1].Value)
	}
}

func TestClampTrendBuckets(t *testing.T) {
	cases := []struct {
		in   int
		want int
	}{
		{0, DefaultTrendBuckets},
		{-4, DefaultTrendBuckets},
		{6, 6},
		{24, 24},
		{999, MaxTrendBuckets},
	}
	for _, tc := range cases {
		if got := ClampTrendBuckets(tc.in); got != tc.want {
			t.Fatalf("ClampTrendBuckets(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}
