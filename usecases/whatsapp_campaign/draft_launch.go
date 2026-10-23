package whatsapp_campaign_usecase

import (
	"context"
	"errors"
	"time"

	"vozko/domain/campaign"
	"vozko/domain/campaign/launchcheck"
	"vozko/domain/campaignaudience"
	"vozko/domain/whatsapp/template"
	wc "vozko/domain/whatsapp_campaign"
	wd "vozko/domain/workspace/workspace_department"
	"vozko/usecases/campaignlaunch"
)

var ErrDraftLaunchUnavailable = errors.New("whatsapp campaign draft launch is not configured")

type AudienceCounter interface {
	Count(ctx context.Context, camp *wc.Campaign) (campaignaudience.Count, error)
}

type DraftLaunchDeps struct {
	Create   wc.CreateCampaignUseCase
	Start    wc.ConfirmedStartUseCase
	Check    *LaunchCheck
	Audience AudienceCounter
}

type DraftLaunch struct {
	d DraftLaunchDeps
}

func NewDraftLaunch(deps DraftLaunchDeps) *DraftLaunch {
	return &DraftLaunch{d: deps}
}

type DraftReview struct {
	Audience campaignaudience.Count
	Quote    *template.SendCost
	Report   launchcheck.Report
}

type DraftLaunchInput struct {
	Campaign             *wc.Campaign
	Launch               campaign.Launch
	ConfirmedFingerprint string
	Departments          *wd.DepartmentFilter
	Settle               func(ctx context.Context, created *wc.Campaign) []string
}

func (u *DraftLaunch) ready() error {
	if u == nil || u.d.Create == nil || u.d.Start == nil || u.d.Check == nil || u.d.Audience == nil {
		return ErrDraftLaunchUnavailable
	}
	return nil
}

func (u *DraftLaunch) Review(ctx context.Context, camp *wc.Campaign) (DraftReview, error) {
	if err := u.ready(); err != nil {
		return DraftReview{}, err
	}
	draft := *camp
	draft.Normalize()
	count, err := u.d.Audience.Count(ctx, &draft)
	if err != nil {
		return DraftReview{}, err
	}
	report, quote := u.d.Check.Preview(ctx, &draft, sendableOf(count))
	return DraftReview{Audience: count, Quote: quote, Report: report}, nil
}

func sendableOf(count campaignaudience.Count) int {
	if count.Sendable == nil {
		return 0
	}
	return *count.Sendable
}

func (u *DraftLaunch) Launch(ctx context.Context, in DraftLaunchInput) (campaignlaunch.Outcome[*wc.Campaign], error) {
	if err := u.ready(); err != nil {
		return campaignlaunch.Outcome[*wc.Campaign]{}, err
	}
	if in.Campaign == nil {
		return campaignlaunch.Outcome[*wc.Campaign]{}, wc.ErrCampaignNameRequired
	}
	return campaignlaunch.Run(ctx, campaignlaunch.Plan[*wc.Campaign]{
		Launch:               in.Launch,
		ConfirmedFingerprint: in.ConfirmedFingerprint,
		Check: func(ctx context.Context) (launchcheck.Report, error) {
			review, err := u.Review(ctx, in.Campaign)
			return review.Report, err
		},
		Create: func(ctx context.Context, scheduledStart time.Time) (*wc.Campaign, error) {
			draft := *in.Campaign
			draft.ScheduledStart = scheduledStart
			return u.d.Create.Execute(ctx, &draft)
		},
		Settle: in.Settle,
		Start: func(_ context.Context, created *wc.Campaign) error {
			_, err := u.d.Start.StartConfirmed(created.WorkspaceID, in.Departments, created.ID, in.ConfirmedFingerprint)
			return err
		},
	})
}
