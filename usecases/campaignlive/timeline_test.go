package campaignlive

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/campaign"
)

type stubSource struct {
	span   campaign.TimelineSpan
	points []campaign.TimelinePoint
	spans  int
	counts int
}

func (s *stubSource) Span(string) (campaign.TimelineSpan, error) {
	s.spans++
	return s.span, nil
}

func (s *stubSource) Count(string, campaign.TimelinePlan) ([]campaign.TimelinePoint, error) {
	s.counts++
	return s.points, nil
}

type fixedLocator struct {
	location *time.Location
}

func (f fixedLocator) Location(context.Context, string, string) (*time.Location, error) {
	return f.location, nil
}

func timelineFixture(t *testing.T, source *stubSource, locator Locator) (*countingGate, campaign.TimelineUseCase) {
	t.Helper()
	gate := &countingGate{}
	now := time.Date(2026, 10, 20, 10, 30, 0, 0, time.UTC)
	uc, err := NewTimeline(TimelineDeps{Namespace: "official", Source: source, Locator: locator, Memo: newMemoryMemo(), Gate: gate, Clock: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("NewTimeline: %v", err)
	}
	return gate, uc
}

func TestARunningCampaignTimelineIsCountedPerMinuteUpToNow(t *testing.T) {
	first := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
	last := time.Date(2026, 10, 20, 10, 20, 0, 0, time.UTC)
	source := &stubSource{span: campaign.TimelineSpan{First: &first, Last: &last}, points: []campaign.TimelinePoint{{At: first, Sent: 7}}}
	gate, uc := timelineFixture(t, source, fixedLocator{location: time.UTC})

	got, err := uc.Execute(context.Background(), running("camp-1"), "")
	if err != nil {
		t.Fatalf("Execute = %v", err)
	}
	if got.Bucket != campaign.TimelineMinute || len(got.Points) != 31 || got.Points[0].Sent != 7 || got.RefreshAfterSeconds != 15 {
		t.Fatalf("timeline = %+v", got)
	}
	if gate.acquired != 1 {
		t.Fatalf("the timeline must go through the analytics gate, acquired %d", gate.acquired)
	}
}

func TestViewersShareOneTimelineRead(t *testing.T) {
	first := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
	source := &stubSource{span: campaign.TimelineSpan{First: &first, Last: &first}}
	_, uc := timelineFixture(t, source, fixedLocator{location: time.UTC})
	for i := 0; i < 10; i++ {
		if _, err := uc.Execute(context.Background(), running("camp-1"), campaign.TimelineHour); err != nil {
			t.Fatalf("Execute = %v", err)
		}
	}
	if source.spans != 1 || source.counts != 1 {
		t.Fatalf("read the span %d and counted %d times, want once each", source.spans, source.counts)
	}
}

func TestACampaignWithNothingStampedSkipsTheCount(t *testing.T) {
	source := &stubSource{}
	_, uc := timelineFixture(t, source, fixedLocator{location: time.UTC})
	got, err := uc.Execute(context.Background(), running("camp-1"), "")
	if err != nil {
		t.Fatalf("Execute = %v", err)
	}
	if source.counts != 0 || len(got.Points) != 0 || got.From != nil {
		t.Fatalf("timeline = %+v after %d counts", got, source.counts)
	}
}

func TestTheTimelineRefusesWhatItCannotPlace(t *testing.T) {
	source := &stubSource{}
	_, uc := timelineFixture(t, source, fixedLocator{location: time.UTC})
	if _, err := uc.Execute(context.Background(), running("camp-1"), "week"); !errors.Is(err, campaign.ErrTimelineBucketInvalid) {
		t.Fatalf("Execute(week) = %v", err)
	}
	_, unplaced := timelineFixture(t, source, fixedLocator{})
	if _, err := unplaced.Execute(context.Background(), running("camp-1"), ""); !errors.Is(err, campaign.ErrTimelineLocationMissing) {
		t.Fatalf("Execute without a clock = %v", err)
	}
	if source.spans != 0 {
		t.Fatal("a refused timeline must not read the database")
	}
	if _, err := NewTimeline(TimelineDeps{Namespace: "official", Source: source, Locator: fixedLocator{}, Memo: newMemoryMemo()}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("a timeline without the analytics gate = %v", err)
	}
}
