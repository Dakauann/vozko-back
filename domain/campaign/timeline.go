package campaign

import (
	"errors"
	"time"
)

type TimelineBucket string

const (
	TimelineMinute TimelineBucket = "minute"
	TimelineHour   TimelineBucket = "hour"
	TimelineDay    TimelineBucket = "day"
)

const (
	minuteTimelineSpan     = 3 * time.Hour
	hourTimelineSpan       = 7 * 24 * time.Hour
	runningTimelineRefresh = 15 * time.Second
)

var (
	ErrTimelineBucketInvalid   = errors.New("campaign timeline: bucket must be minute, hour or day")
	ErrTimelineLocationMissing = errors.New("campaign timeline: the workspace clock is unknown")
)

var timelineMaxPoints = map[TimelineBucket]int{
	TimelineMinute: 180,
	TimelineHour:   168,
	TimelineDay:    92,
}

func TimelineMaxPoints(bucket TimelineBucket) int {
	return timelineMaxPoints[bucket]
}

func (b TimelineBucket) Valid() bool {
	_, ok := timelineMaxPoints[b]
	return ok
}

type TimelineSpan struct {
	First *time.Time
	Last  *time.Time
}

type TimelinePlan struct {
	Bucket       TimelineBucket
	From         time.Time
	To           time.Time
	Location     *time.Location
	RefreshAfter time.Duration
	empty        bool
}

type TimelinePoint struct {
	At        time.Time `json:"at"`
	Sent      int64     `json:"sent"`
	Delivered int64     `json:"delivered"`
	Read      int64     `json:"read"`
	Failed    int64     `json:"failed"`
}

type Timeline struct {
	Bucket              TimelineBucket  `json:"bucket"`
	From                *time.Time      `json:"from,omitempty"`
	To                  *time.Time      `json:"to,omitempty"`
	Timezone            string          `json:"timezone"`
	Points              []TimelinePoint `json:"points"`
	RefreshAfterSeconds int             `json:"refreshAfterSeconds"`
}

func PlanTimeline(span TimelineSpan, requested TimelineBucket, status Status, now time.Time, location *time.Location) (TimelinePlan, error) {
	if location == nil {
		return TimelinePlan{}, ErrTimelineLocationMissing
	}
	if requested != "" && !requested.Valid() {
		return TimelinePlan{}, ErrTimelineBucketInvalid
	}
	plan := TimelinePlan{Bucket: requested, Location: location}
	if status == StatusRunning {
		plan.RefreshAfter = runningTimelineRefresh
	}
	if span.First == nil || span.Last == nil {
		if plan.Bucket == "" {
			plan.Bucket = TimelineMinute
		}
		plan.empty = true
		return plan, nil
	}

	end := *span.Last
	if status == StatusRunning && now.After(end) {
		end = now
	}
	if plan.Bucket == "" {
		plan.Bucket = fittingBucket(end.Sub(*span.First))
	}
	earliest := plan.back(plan.truncate(end), TimelineMaxPoints(plan.Bucket)-1)
	from := *span.First
	if from.Before(earliest) {
		from = earliest
	}
	plan.From = plan.truncate(from)
	plan.To = end
	return plan, nil
}

func fittingBucket(span time.Duration) TimelineBucket {
	switch {
	case span <= minuteTimelineSpan:
		return TimelineMinute
	case span <= hourTimelineSpan:
		return TimelineHour
	default:
		return TimelineDay
	}
}

func (p TimelinePlan) Empty() bool {
	return p.empty
}

func (p TimelinePlan) Starts() []time.Time {
	if p.empty {
		return nil
	}
	last := p.truncate(p.To)
	var starts []time.Time
	for start := p.truncate(p.From); !start.After(last); start = p.next(start) {
		starts = append(starts, start)
	}
	return starts
}

func (p TimelinePlan) truncate(t time.Time) time.Time {
	local := t.In(p.Location)
	switch p.Bucket {
	case TimelineDay:
		return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, p.Location)
	case TimelineHour:
		return time.Date(local.Year(), local.Month(), local.Day(), local.Hour(), 0, 0, 0, p.Location)
	default:
		return time.Date(local.Year(), local.Month(), local.Day(), local.Hour(), local.Minute(), 0, 0, p.Location)
	}
}

func (p TimelinePlan) next(start time.Time) time.Time {
	switch p.Bucket {
	case TimelineDay:
		return start.AddDate(0, 0, 1)
	case TimelineHour:
		return start.Add(time.Hour)
	default:
		return start.Add(time.Minute)
	}
}

func (p TimelinePlan) back(start time.Time, steps int) time.Time {
	switch p.Bucket {
	case TimelineDay:
		return start.AddDate(0, 0, -steps)
	case TimelineHour:
		return start.Add(-time.Duration(steps) * time.Hour)
	default:
		return start.Add(-time.Duration(steps) * time.Minute)
	}
}

func FillTimeline(plan TimelinePlan, counted []TimelinePoint) []TimelinePoint {
	byStart := make(map[int64]TimelinePoint, len(counted))
	for _, point := range counted {
		byStart[point.At.Unix()] = point
	}
	starts := plan.Starts()
	points := make([]TimelinePoint, 0, len(starts))
	for _, start := range starts {
		point := byStart[start.Unix()]
		point.At = start
		points = append(points, point)
	}
	return points
}

func NewTimeline(plan TimelinePlan, counted []TimelinePoint) Timeline {
	timeline := Timeline{
		Bucket:              plan.Bucket,
		Timezone:            plan.Location.String(),
		Points:              FillTimeline(plan, counted),
		RefreshAfterSeconds: int(plan.RefreshAfter / time.Second),
	}
	if !plan.empty {
		from, to := plan.From, plan.To
		timeline.From, timeline.To = &from, &to
	}
	return timeline
}

func (p *TimelinePoint) Add(milestone Milestone, count int64) {
	switch milestone {
	case MilestoneSent:
		p.Sent += count
	case MilestoneDelivered:
		p.Delivered += count
	case MilestoneRead:
		p.Read += count
	case MilestoneFailed:
		p.Failed += count
	}
}
