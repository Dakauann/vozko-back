package campaign

import (
	"errors"
	"testing"
	"time"
)

func at(hour, minute int) time.Time {
	return time.Date(2026, 10, 20, hour, minute, 0, 0, time.UTC)
}

func TestTheTimelinePicksTheFinestBucketThatFitsTheSendingSpan(t *testing.T) {
	cases := []struct {
		name  string
		first time.Time
		last  time.Time
		want  TimelineBucket
	}{
		{name: "an hour of sending", first: at(10, 0), last: at(11, 0), want: TimelineMinute},
		{name: "three hours of sending", first: at(8, 0), last: at(11, 0), want: TimelineMinute},
		{name: "a working day", first: at(8, 0), last: at(18, 0), want: TimelineHour},
		{name: "a week", first: at(8, 0).AddDate(0, 0, -6), last: at(18, 0), want: TimelineHour},
		{name: "a month", first: at(8, 0).AddDate(0, -1, 0), last: at(18, 0), want: TimelineDay},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := PlanTimeline(TimelineSpan{First: &tc.first, Last: &tc.last}, "", StatusCompleted, at(20, 0), time.UTC)
			if err != nil {
				t.Fatalf("PlanTimeline = %v", err)
			}
			if plan.Bucket != tc.want {
				t.Fatalf("bucket = %s, want %s", plan.Bucket, tc.want)
			}
		})
	}
}

func TestARequestedBucketShowsItsLatestWindow(t *testing.T) {
	first, last := at(8, 0).AddDate(0, 0, -2), at(18, 30)
	plan, err := PlanTimeline(TimelineSpan{First: &first, Last: &last}, TimelineMinute, StatusCompleted, at(20, 0), time.UTC)
	if err != nil {
		t.Fatalf("PlanTimeline = %v", err)
	}
	if plan.Bucket != TimelineMinute || !plan.To.Equal(at(18, 30)) || !plan.From.Equal(at(15, 31)) {
		t.Fatalf("plan = %+v", plan)
	}
	if got := len(plan.Starts()); got != TimelineMaxPoints(TimelineMinute) {
		t.Fatalf("%d minute buckets, want %d", got, TimelineMaxPoints(TimelineMinute))
	}
}

func TestARunningCampaignTimelineReachesNow(t *testing.T) {
	first, last := at(10, 0), at(10, 20)
	plan, err := PlanTimeline(TimelineSpan{First: &first, Last: &last}, "", StatusRunning, at(10, 45), time.UTC)
	if err != nil {
		t.Fatalf("PlanTimeline = %v", err)
	}
	if !plan.To.Equal(at(10, 45)) || !plan.From.Equal(at(10, 0)) || plan.RefreshAfter != 15*time.Second {
		t.Fatalf("plan = %+v", plan)
	}
}

func TestATimelineWithNothingSentYetIsEmptyButValid(t *testing.T) {
	plan, err := PlanTimeline(TimelineSpan{}, "", StatusStopped, at(10, 0), time.UTC)
	if err != nil {
		t.Fatalf("PlanTimeline = %v", err)
	}
	if plan.Bucket != TimelineMinute || len(plan.Starts()) != 0 {
		t.Fatalf("plan = %+v", plan)
	}
}

func TestAnUnknownBucketIsRefused(t *testing.T) {
	if _, err := PlanTimeline(TimelineSpan{}, "week", StatusStopped, at(10, 0), time.UTC); !errors.Is(err, ErrTimelineBucketInvalid) {
		t.Fatalf("PlanTimeline = %v, want ErrTimelineBucketInvalid", err)
	}
	if _, err := PlanTimeline(TimelineSpan{}, "", StatusStopped, at(10, 0), nil); !errors.Is(err, ErrTimelineLocationMissing) {
		t.Fatalf("PlanTimeline without a location = %v", err)
	}
}

func TestDayBucketsFollowTheWorkspaceClock(t *testing.T) {
	saoPaulo := time.FixedZone("BRT", -3*60*60)
	first := time.Date(2026, 9, 1, 2, 0, 0, 0, time.UTC)
	last := time.Date(2026, 10, 20, 2, 0, 0, 0, time.UTC)
	plan, err := PlanTimeline(TimelineSpan{First: &first, Last: &last}, TimelineDay, StatusCompleted, last, saoPaulo)
	if err != nil {
		t.Fatalf("PlanTimeline = %v", err)
	}
	starts := plan.Starts()
	if starts[0] != time.Date(2026, 8, 31, 0, 0, 0, 0, saoPaulo) || starts[len(starts)-1] != time.Date(2026, 10, 19, 0, 0, 0, 0, saoPaulo) {
		t.Fatalf("day starts run %v to %v", starts[0], starts[len(starts)-1])
	}
}

func TestTheTimelineFillsQuietBucketsWithZero(t *testing.T) {
	first, last := at(10, 0), at(10, 3)
	plan, err := PlanTimeline(TimelineSpan{First: &first, Last: &last}, TimelineMinute, StatusCompleted, at(11, 0), time.UTC)
	if err != nil {
		t.Fatalf("PlanTimeline = %v", err)
	}
	points := FillTimeline(plan, []TimelinePoint{{At: at(10, 1), Sent: 4, Delivered: 3}, {At: at(9, 0), Sent: 99}})
	if len(points) != 4 || points[0].Sent != 0 || points[1].Sent != 4 || points[1].Delivered != 3 || points[3].At != at(10, 3) {
		t.Fatalf("points = %+v", points)
	}
}

func TestAMilestoneCountLandsOnItsOwnSeries(t *testing.T) {
	var point TimelinePoint
	point.Add(MilestoneSent, 5)
	point.Add(MilestoneDelivered, 4)
	point.Add(MilestoneRead, 2)
	point.Add(MilestoneFailed, 1)
	point.Add(Milestone("unknown"), 9)
	if point.Sent != 5 || point.Delivered != 4 || point.Read != 2 || point.Failed != 1 {
		t.Fatalf("point = %+v", point)
	}
}
