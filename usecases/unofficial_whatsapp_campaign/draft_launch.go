package unofficial_whatsapp_campaign

import (
	"context"
	"errors"
	"time"

	"vozko/domain/campaign"
	"vozko/domain/campaign/launchcheck"
	"vozko/domain/campaignaudience"
	uw "vozko/domain/unofficial_whatsapp"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
	"vozko/usecases/campaignlaunch"
)

var ErrDraftLaunchUnavailable = errors.New("unofficial whatsapp campaign draft launch is not configured")

type AudienceCounter interface {
	Count(ctx context.Context, camp *uwc.Campaign) (campaignaudience.Count, error)
}

type DraftLaunchDeps struct {
	Create    uwc.CreateCampaignUseCase
	Actions   uwc.CampaignActionUseCase
	Estimate  uwc.EstimateCampaignUseCase
	Audience  AudienceCounter
	Preflight *StartPreflight
}

type DraftLaunch struct {
	d DraftLaunchDeps
}

func NewDraftLaunch(deps DraftLaunchDeps) *DraftLaunch {
	return &DraftLaunch{d: deps}
}

type DraftReview struct {
	Audience campaignaudience.Count
	Estimate *uwc.DraftEstimate
	Report   launchcheck.Report
}

type DraftLaunchInput struct {
	Campaign *uwc.Campaign
	Launch   campaign.Launch
	Scope    uw.DepartmentScope
	Settle   func(ctx context.Context, created *uwc.Campaign) []string
}

func (u *DraftLaunch) ready() error {
	if u == nil || u.d.Create == nil || u.d.Actions == nil || u.d.Estimate == nil || u.d.Audience == nil || u.d.Preflight == nil {
		return ErrDraftLaunchUnavailable
	}
	return nil
}

func (u *DraftLaunch) Review(ctx context.Context, camp *uwc.Campaign, scope uw.DepartmentScope) (DraftReview, error) {
	if err := u.ready(); err != nil {
		return DraftReview{}, err
	}
	draft := *camp
	draft.Normalize()
	count, err := u.countOf(ctx, &draft)
	if err != nil {
		return DraftReview{}, err
	}
	pending := 0
	if count.Sendable != nil {
		pending = *count.Sendable
	}
	estimate, estimateErr := u.d.Estimate.Draft(ctx, estimateInputOf(camp, pending), scope)
	if estimateErr != nil {
		estimate = nil
	}
	refusal := u.preflight(ctx, &draft, estimateErr)
	return DraftReview{Audience: count, Estimate: estimate, Report: uwc.StartReport(pending, refusal, unsendableGroups(&draft, estimate))}, nil
}

func (u *DraftLaunch) countOf(ctx context.Context, draft *uwc.Campaign) (campaignaudience.Count, error) {
	if draft.TargetsGroups() {
		n := len(draft.Targets)
		return campaignaudience.Count{Channel: campaignaudience.ChannelUnofficial, Matched: n, WithContact: n, Sendable: &n}, nil
	}
	return u.d.Audience.Count(ctx, draft)
}

func (u *DraftLaunch) preflight(ctx context.Context, draft *uwc.Campaign, estimateErr error) error {
	if draft.TargetsGroups() {
		return estimateErr
	}
	return u.d.Preflight.Check(ctx, draft)
}

func unsendableGroups(draft *uwc.Campaign, estimate *uwc.DraftEstimate) int {
	if !draft.TargetsGroups() || estimate == nil {
		return 0
	}
	count := 0
	for _, group := range estimate.Groups {
		if len(group.Senders) == 0 || group.Refusal() != nil {
			count++
		}
	}
	return count
}

func estimateInputOf(draft *uwc.Campaign, pending int) uwc.EstimateInput {
	in := uwc.EstimateInput{
		WorkspaceID: draft.WorkspaceID, Audience: draft.Audience, InstanceID: draft.InstanceID,
		SendDelayMinMS: draft.SendDelayMinMS, SendDelayMaxMS: draft.SendDelayMaxMS, Targets: pending,
	}
	if draft.TargetsGroups() {
		for _, target := range draft.Targets {
			in.GroupJIDs = append(in.GroupJIDs, target.GroupJID)
		}
	}
	return in
}

func (u *DraftLaunch) Launch(ctx context.Context, in DraftLaunchInput) (campaignlaunch.Outcome[*uwc.Campaign], error) {
	if err := u.ready(); err != nil {
		return campaignlaunch.Outcome[*uwc.Campaign]{}, err
	}
	if in.Campaign == nil {
		return campaignlaunch.Outcome[*uwc.Campaign]{}, uwc.ErrCampaignNameRequired
	}
	return campaignlaunch.Run(ctx, campaignlaunch.Plan[*uwc.Campaign]{
		Launch: in.Launch,
		Check: func(ctx context.Context) (launchcheck.Report, error) {
			review, err := u.Review(ctx, in.Campaign, in.Scope)
			return review.Report, err
		},
		Create: func(ctx context.Context, scheduledStart time.Time) (*uwc.Campaign, error) {
			draft := *in.Campaign
			draft.ScheduledStart = scheduledStart
			return u.d.Create.Execute(ctx, &draft, in.Scope)
		},
		Settle: in.Settle,
		Start: func(ctx context.Context, created *uwc.Campaign) error {
			_, err := u.d.Actions.Act(ctx, created.WorkspaceID, in.Scope, created.ID, campaign.ActionStart)
			return err
		},
	})
}
