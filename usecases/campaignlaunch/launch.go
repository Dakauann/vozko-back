package campaignlaunch

import (
	"context"
	"errors"
	"time"

	"vozko/domain/campaign"
	"vozko/domain/campaign/launchcheck"
)

var (
	ErrPlanIncomplete = errors.New("campaign launch: the plan is missing a step")
)

type Plan[C any] struct {
	Launch               campaign.Launch
	ConfirmedFingerprint string
	Check                func(ctx context.Context) (launchcheck.Report, error)
	Create               func(ctx context.Context, scheduledStart time.Time) (C, error)
	Settle               func(ctx context.Context, created C) []string
	Start                func(ctx context.Context, created C) error
}

type Outcome[C any] struct {
	Campaign     C
	Started      bool
	Warnings     []string
	StartRefusal error
}

func (p Plan[C]) complete() bool {
	return p.Create != nil && (!p.Launch.StartsNow() || (p.Check != nil && p.Start != nil))
}

func Run[C any](ctx context.Context, p Plan[C]) (Outcome[C], error) {
	var none Outcome[C]
	if !p.complete() {
		return none, ErrPlanIncomplete
	}
	if p.Launch.StartsNow() {
		report, err := p.Check(ctx)
		if err != nil {
			return none, err
		}
		if err := report.Admit(p.ConfirmedFingerprint); err != nil {
			return none, err
		}
	}
	var at time.Time
	if p.Launch.Mode == campaign.LaunchSchedule {
		at = p.Launch.At
	}
	created, err := p.Create(ctx, at)
	if err != nil {
		return none, err
	}
	out := Outcome[C]{Campaign: created}
	if p.Settle != nil {
		out.Warnings = p.Settle(ctx, created)
	}
	if !p.Launch.StartsNow() {
		return out, nil
	}
	if len(out.Warnings) > 0 {
		out.StartRefusal = campaign.ErrSettingsNotApplied
		return out, nil
	}
	if err := p.Start(ctx, created); err != nil {
		out.StartRefusal = err
		return out, nil
	}
	out.Started = true
	return out, nil
}
