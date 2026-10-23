package campaignlaunch

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/campaign"
	"vozko/domain/campaign/launchcheck"
)

type recorder struct {
	checked   int
	created   int
	settled   int
	started   int
	createdAt time.Time
	report    launchcheck.Report
	checkErr  error
	createErr error
	startErr  error
	warnings  []string
}

func (r *recorder) plan(l campaign.Launch, fingerprint string) Plan[string] {
	return Plan[string]{
		Launch:               l,
		ConfirmedFingerprint: fingerprint,
		Check: func(context.Context) (launchcheck.Report, error) {
			r.checked++
			return r.report, r.checkErr
		},
		Create: func(_ context.Context, at time.Time) (string, error) {
			r.created++
			r.createdAt = at
			if r.createErr != nil {
				return "", r.createErr
			}
			return "c1", nil
		},
		Settle: func(context.Context, string) []string {
			r.settled++
			return r.warnings
		},
		Start: func(context.Context, string) error {
			r.started++
			return r.startErr
		},
	}
}

var blocked = launchcheck.Report{Findings: []launchcheck.Finding{{Code: launchcheck.CodeTemplateNotReady, Level: launchcheck.Block}}}
var risky = launchcheck.Report{Findings: []launchcheck.Finding{{Code: launchcheck.CodeNoExit, Level: launchcheck.Confirm}}}

func TestReadyCreatesWithoutCheckingOrStarting(t *testing.T) {
	r := &recorder{report: blocked}
	out, err := Run(context.Background(), r.plan(campaign.Launch{Mode: campaign.LaunchReady}, ""))
	if err != nil {
		t.Fatal(err)
	}
	if out.Campaign != "c1" || out.Started || r.checked != 0 || r.started != 0 || r.settled != 1 || !r.createdAt.IsZero() {
		t.Fatalf("out %+v recorder %+v", out, r)
	}
}

func TestScheduleCreatesWithTheTimeAndNeverStarts(t *testing.T) {
	at := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	r := &recorder{}
	out, err := Run(context.Background(), r.plan(campaign.Launch{Mode: campaign.LaunchSchedule, At: at}, ""))
	if err != nil {
		t.Fatal(err)
	}
	if out.Started || r.started != 0 || r.checked != 0 || !r.createdAt.Equal(at) {
		t.Fatalf("out %+v recorder %+v", out, r)
	}
}

func TestNowChecksCreatesSettlesAndStarts(t *testing.T) {
	r := &recorder{}
	out, err := Run(context.Background(), r.plan(campaign.Launch{Mode: campaign.LaunchNow}, ""))
	if err != nil {
		t.Fatal(err)
	}
	if !out.Started || r.checked != 1 || r.created != 1 || r.settled != 1 || r.started != 1 || !r.createdAt.IsZero() {
		t.Fatalf("out %+v recorder %+v", out, r)
	}
}

func TestNowWithABlockCreatesNothing(t *testing.T) {
	r := &recorder{report: blocked}
	_, err := Run(context.Background(), r.plan(campaign.Launch{Mode: campaign.LaunchNow}, ""))
	if !errors.Is(err, launchcheck.ErrBlocked) {
		t.Fatalf("got %v", err)
	}
	if r.created != 0 || r.started != 0 {
		t.Fatalf("recorder %+v", r)
	}
}

func TestNowWithAnUnconfirmedRiskCreatesNothing(t *testing.T) {
	r := &recorder{report: risky}
	_, err := Run(context.Background(), r.plan(campaign.Launch{Mode: campaign.LaunchNow}, "stale"))
	if !errors.Is(err, launchcheck.ErrUnconfirmed) {
		t.Fatalf("got %v", err)
	}
	if r.created != 0 {
		t.Fatal("created after an unconfirmed risk")
	}
}

func TestNowWithTheConfirmedRiskStarts(t *testing.T) {
	r := &recorder{report: risky}
	out, err := Run(context.Background(), r.plan(campaign.Launch{Mode: campaign.LaunchNow}, risky.Fingerprint()))
	if err != nil || !out.Started {
		t.Fatalf("out %+v err %v", out, err)
	}
}

func TestNowWhenTheCheckCannotRunCreatesNothing(t *testing.T) {
	boom := errors.New("boom")
	r := &recorder{checkErr: boom}
	if _, err := Run(context.Background(), r.plan(campaign.Launch{Mode: campaign.LaunchNow}, "")); !errors.Is(err, boom) {
		t.Fatalf("got %v", err)
	}
	if r.created != 0 {
		t.Fatal("created without a check")
	}
}

func TestNowKeepsTheCampaignReadyWhenSettingsFail(t *testing.T) {
	r := &recorder{warnings: []string{"deal_automation_not_applied"}}
	out, err := Run(context.Background(), r.plan(campaign.Launch{Mode: campaign.LaunchNow}, ""))
	if err != nil {
		t.Fatal(err)
	}
	if out.Started || r.started != 0 || !errors.Is(out.StartRefusal, campaign.ErrSettingsNotApplied) || len(out.Warnings) != 1 {
		t.Fatalf("out %+v", out)
	}
}

func TestNowKeepsTheCampaignReadyWhenTheStartRefuses(t *testing.T) {
	refusal := &launchcheck.RefusedError{Report: blocked}
	r := &recorder{startErr: refusal}
	out, err := Run(context.Background(), r.plan(campaign.Launch{Mode: campaign.LaunchNow}, ""))
	if err != nil {
		t.Fatal(err)
	}
	if out.Started || out.Campaign != "c1" || !errors.Is(out.StartRefusal, launchcheck.ErrBlocked) {
		t.Fatalf("out %+v", out)
	}
}

func TestACreateFailureIsReturned(t *testing.T) {
	boom := errors.New("invalid")
	r := &recorder{createErr: boom}
	if _, err := Run(context.Background(), r.plan(campaign.Launch{Mode: campaign.LaunchReady}, "")); !errors.Is(err, boom) {
		t.Fatalf("got %v", err)
	}
	if r.settled != 0 {
		t.Fatal("settled a campaign that was not created")
	}
}

func TestAnIncompletePlanIsRefused(t *testing.T) {
	if _, err := Run(context.Background(), Plan[string]{Launch: campaign.Launch{Mode: campaign.LaunchReady}}); !errors.Is(err, ErrPlanIncomplete) {
		t.Fatalf("got %v", err)
	}
	p := (&recorder{}).plan(campaign.Launch{Mode: campaign.LaunchNow}, "")
	p.Check = nil
	if _, err := Run(context.Background(), p); !errors.Is(err, ErrPlanIncomplete) {
		t.Fatalf("now without a check: %v", err)
	}
}
