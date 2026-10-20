package campaignlive

import (
	"context"
	"fmt"
	"time"

	"vozko/domain/cache"
	"vozko/domain/campaign"
)

const (
	runningTimelineFreshness = 10 * time.Second
	idleTimelineFreshness    = 5 * time.Minute
)

type TimelineSource interface {
	Span(campaignID string) (campaign.TimelineSpan, error)
	Count(campaignID string, plan campaign.TimelinePlan) ([]campaign.TimelinePoint, error)
}

type Locator interface {
	Location(ctx context.Context, workspaceID, departmentID string) (*time.Location, error)
}

type TimelineDeps struct {
	Namespace string
	Source    TimelineSource
	Locator   Locator
	Memo      cache.Memo
	Gate      cache.Gate
	Clock     func() time.Time
}

type timelineUseCase struct {
	deps TimelineDeps
}

func NewTimeline(deps TimelineDeps) (campaign.TimelineUseCase, error) {
	if deps.Namespace == "" || deps.Source == nil || deps.Locator == nil || deps.Memo == nil || deps.Gate == nil {
		return nil, ErrUnavailable
	}
	if deps.Clock == nil {
		deps.Clock = time.Now
	}
	return &timelineUseCase{deps: deps}, nil
}

func (uc *timelineUseCase) Execute(ctx context.Context, live campaign.LiveCampaign, bucket campaign.TimelineBucket) (*campaign.Timeline, error) {
	if live.ID == "" {
		return nil, ErrCampaignMissing
	}
	if bucket != "" && !bucket.Valid() {
		return nil, campaign.ErrTimelineBucketInvalid
	}
	location, err := uc.deps.Locator.Location(ctx, live.WorkspaceID, live.DepartmentID)
	if err != nil {
		return nil, err
	}
	if location == nil {
		return nil, campaign.ErrTimelineLocationMissing
	}
	key := fmt.Sprintf("%s:timeline:%s:%s:%s:%s", uc.deps.Namespace, live.ID, live.Status, bucket, location)
	timeline, err := cache.Remember(ctx, uc.deps.Memo, key, timelineFreshness(live.Status), func(ctx context.Context) (campaign.Timeline, error) {
		var out campaign.Timeline
		err := cache.Gated(ctx, uc.deps.Gate, func(context.Context) error {
			var err error
			out, err = uc.read(live, bucket, location)
			return err
		})
		return out, err
	})
	if err != nil {
		return nil, err
	}
	return &timeline, nil
}

func (uc *timelineUseCase) read(live campaign.LiveCampaign, bucket campaign.TimelineBucket, location *time.Location) (campaign.Timeline, error) {
	span, err := uc.deps.Source.Span(live.ID)
	if err != nil {
		return campaign.Timeline{}, err
	}
	plan, err := campaign.PlanTimeline(span, bucket, live.Status, uc.deps.Clock(), location)
	if err != nil {
		return campaign.Timeline{}, err
	}
	if plan.Empty() {
		return campaign.NewTimeline(plan, nil), nil
	}
	counted, err := uc.deps.Source.Count(live.ID, plan)
	if err != nil {
		return campaign.Timeline{}, err
	}
	return campaign.NewTimeline(plan, counted), nil
}

func timelineFreshness(status campaign.Status) time.Duration {
	if status == campaign.StatusRunning {
		return runningTimelineFreshness
	}
	return idleTimelineFreshness
}
