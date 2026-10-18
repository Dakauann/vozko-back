package whatsapp_campaign_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/campaign/launchcheck"
	wc "vozko/domain/whatsapp_campaign"
)

type fixedGate struct {
	report launchcheck.Report
	asked  int
}

func (g *fixedGate) Run(context.Context, string) (launchcheck.Report, error) {
	g.asked++
	return g.report, nil
}

func passingGate() *fixedGate { return &fixedGate{} }

func riskyGate() *fixedGate {
	return &fixedGate{report: launchcheck.Report{Findings: []launchcheck.Finding{{Code: launchcheck.CodeTemplateRed, Level: launchcheck.Confirm}}}}
}

func blockedGate() *fixedGate {
	return &fixedGate{report: launchcheck.Report{Findings: []launchcheck.Finding{{Code: launchcheck.CodePhoneUnavailable, Level: launchcheck.Block}}}}
}

func TestABlockedCampaignDoesNotStartAndNothingChanges(t *testing.T) {
	r := newDispatchRig(wc.CampaignStatusStopped)
	r.dispatch.SetLaunchGate(blockedGate())

	err := r.dispatch.Dispatch(wc.DispatchCampaignInput{CampaignID: "c1", Action: wc.CampaignActionStart})

	if !errors.Is(err, launchcheck.ErrBlocked) || len(r.repo.statusCalls) != 0 || r.pub.count() != 0 {
		t.Fatalf("err %v, status calls %v, published %d", err, r.repo.statusCalls, r.pub.count())
	}
}

func TestARiskStartsOnlyWithTheCurrentConfirmation(t *testing.T) {
	r := newDispatchRig(wc.CampaignStatusStopped)
	gate := riskyGate()
	r.dispatch.SetLaunchGate(gate)

	if err := r.dispatch.Dispatch(wc.DispatchCampaignInput{CampaignID: "c1", Action: wc.CampaignActionStart}); !errors.Is(err, launchcheck.ErrUnconfirmed) {
		t.Fatalf("unconfirmed start = %v", err)
	}
	confirmed := wc.DispatchCampaignInput{CampaignID: "c1", Action: wc.CampaignActionStart, ConfirmedFingerprint: gate.report.Fingerprint()}
	if err := r.dispatch.Dispatch(confirmed); err != nil {
		t.Fatalf("confirmed start = %v", err)
	}
	if r.pub.count() == 0 {
		t.Fatal("a confirmed start must send")
	}
}

func TestAStartWithoutALaunchCheckIsRefused(t *testing.T) {
	r := newDispatchRig(wc.CampaignStatusStopped)
	r.dispatch.SetLaunchGate(nil)

	if err := r.dispatch.Dispatch(wc.DispatchCampaignInput{CampaignID: "c1", Action: wc.CampaignActionStart}); !errors.Is(err, wc.ErrLaunchCheckUnavailable) {
		t.Fatalf("err = %v, want the start refused", err)
	}
}

func TestPausingAndStoppingNeverAskTheLaunchCheck(t *testing.T) {
	r := newDispatchRig(wc.CampaignStatusRunning)
	gate := blockedGate()
	r.dispatch.SetLaunchGate(gate)

	if err := r.dispatch.Dispatch(wc.DispatchCampaignInput{CampaignID: "c1", Action: wc.CampaignActionPause}); err != nil {
		t.Fatalf("pause = %v", err)
	}
	if gate.asked != 0 {
		t.Fatal("pausing must not depend on the launch check")
	}
}

func TestASmartCampaignIsCheckedBeforeTheWaveEngineTakesIt(t *testing.T) {
	r := newDispatchRig(wc.CampaignStatusStopped)
	r.smart.managed["c1"] = true
	r.dispatch.SetLaunchGate(blockedGate())

	if err := r.dispatch.Dispatch(wc.DispatchCampaignInput{CampaignID: "c1", Action: wc.CampaignActionStart}); !errors.Is(err, launchcheck.ErrBlocked) || len(r.smart.started) != 0 {
		t.Fatalf("err %v, started %v", err, r.smart.started)
	}
}

func passingDispatch(campaigns wc.Repository) *dispatchCampaignUseCase {
	uc := NewDispatchCampaignUseCase(nil, campaigns, newMockEntryRepo(), nil, nil).(*dispatchCampaignUseCase)
	uc.SetLaunchGate(passingGate())
	return uc
}
