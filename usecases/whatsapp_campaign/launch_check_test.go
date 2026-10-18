package whatsapp_campaign_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/balance"
	"vozko/domain/campaign"
	"vozko/domain/campaign/launchcheck"
	businessphone "vozko/domain/whatsapp/business_phone"
	"vozko/domain/whatsapp/metaerror"
	"vozko/domain/whatsapp/quality"
	"vozko/domain/whatsapp/template"
	wc "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
)

type launchCampaigns map[string]*wc.Campaign

func (l launchCampaigns) FindByID(id string) (*wc.Campaign, error) {
	if c, ok := l[id]; ok {
		return c, nil
	}
	return nil, wc.ErrCampaignNotFound
}

type launchCounts struct{ counts wce.StatusCounts }

func (l launchCounts) CountByStatus(string) (*wce.StatusCounts, error) { return &l.counts, nil }

type launchFailures []wce.FailureReasonCount

func (l launchFailures) FailureReasons(wce.ReportScope) ([]wce.FailureReasonCount, error) {
	return l, nil
}

type launchTemplates map[string]*template.Template

func (l launchTemplates) FindByID(id string) (*template.Template, error) { return l[id], nil }

type launchQuality struct {
	latest map[quality.Signal]quality.Change
	err    error
}

func (l launchQuality) LatestBySignal(quality.Subject, string) (map[quality.Signal]quality.Change, error) {
	return l.latest, l.err
}

type launchPhones map[string]*businessphone.WhatsAppBusinessPhoneNumber

func (l launchPhones) FindByID(id string) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	return l[id], nil
}

type launchExits struct{ configured bool }

func (l launchExits) ExitConfigured(context.Context, string, string, string) (bool, error) {
	return l.configured, nil
}

type launchCosts struct{ unit int64 }

func (l launchCosts) GetTemplateCostMicros(string, string) (int64, error) { return l.unit, nil }

type launchBalances struct{ micros int64 }

func (l launchBalances) GetBalance(string) (int64, error) { return l.micros, nil }

type launchCaps struct{}

func (launchCaps) MonthlySendCapUsage(string, time.Time) (*balance.SendCapUsage, error) { return nil, nil }

func launchDeps() LaunchCheckDeps {
	return LaunchCheckDeps{
		Campaigns:      launchCampaigns{"camp-1": {ID: "camp-1", WorkspaceID: "ws-1", TemplateID: "tpl-1", BusinessPhoneID: "phone-1", TemplateCategory: "UTILITY"}},
		Counts:         launchCounts{counts: wce.StatusCounts{Total: 100, Pending: 100}},
		Failures:       launchFailures{},
		Templates:      launchTemplates{"tpl-1": {ID: "tpl-1", ExternalID: "x1", Status: template.TemplateStatusApproved, Category: template.TemplateCategoryUtility}},
		Quality:        launchQuality{latest: map[quality.Signal]quality.Change{quality.SignalQuality: {Current: "GREEN"}}},
		Phones:         launchPhones{"phone-1": {ID: "phone-1", OwnerWorkspaceID: "ws-1", Status: businessphone.StatusConnected}},
		TemplateGrants: allowGrants{},
		Exits:          launchExits{configured: true},
		Costs:          launchCosts{unit: 1000},
		Balances:       launchBalances{micros: 1_000_000_000},
		Caps:           launchCaps{},
		Now:            func() time.Time { return time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC) },
	}
}

func runCheck(t *testing.T, deps LaunchCheckDeps) launchcheck.Report {
	t.Helper()
	report, err := NewLaunchCheck(deps).Run(context.Background(), "camp-1")
	if err != nil {
		t.Fatalf("Run = %v", err)
	}
	return report
}

func found(r launchcheck.Report, level launchcheck.Level, code launchcheck.Code) bool {
	for _, f := range r.Findings {
		if f.Level == level && f.Code == code {
			return true
		}
	}
	return false
}

func TestAHealthyCampaignStartsWithoutFindings(t *testing.T) {
	if report := runCheck(t, launchDeps()); len(report.Findings) != 0 {
		t.Fatalf("findings = %+v", report.Findings)
	}
}

func TestTheTemplateQualityMetaGaveIsChecked(t *testing.T) {
	deps := launchDeps()
	deps.Quality = launchQuality{latest: map[quality.Signal]quality.Change{quality.SignalQuality: {Current: "RED"}}}

	if !found(runCheck(t, deps), launchcheck.Confirm, launchcheck.CodeTemplateRed) {
		t.Fatal("a RED template must be confirmed")
	}
}

func TestANumberThatCannotSendBlocks(t *testing.T) {
	deps := launchDeps()
	deps.Phones = launchPhones{"phone-1": {ID: "phone-1", OwnerWorkspaceID: "ws-1", Status: businessphone.StatusRestricted}}

	if !found(runCheck(t, deps), launchcheck.Block, launchcheck.CodePhoneUnavailable) {
		t.Fatal("a restricted number must block")
	}
}

func TestARevokedTemplateGrantBlocks(t *testing.T) {
	deps := launchDeps()
	deps.TemplateGrants = &denyGrants{}

	if !found(runCheck(t, deps), launchcheck.Block, launchcheck.CodeAccessRevoked) {
		t.Fatal("a workspace that lost the template must not start")
	}
}

func TestAPhoneOwnedByAnotherWorkspaceBlocks(t *testing.T) {
	deps := launchDeps()
	deps.Phones = launchPhones{"phone-1": {ID: "phone-1", OwnerWorkspaceID: "ws-other", Status: businessphone.StatusConnected}}

	if !found(runCheck(t, deps), launchcheck.Block, launchcheck.CodeAccessRevoked) {
		t.Fatal("a number owned elsewhere must not send")
	}
}

func TestHealthThatCannotBeReadBlocksInsteadOfPassing(t *testing.T) {
	deps := launchDeps()
	deps.Quality = launchQuality{err: errors.New("db down")}

	if !found(runCheck(t, deps), launchcheck.Block, launchcheck.CodeHealthUnreadable) {
		t.Fatal("an unreadable signal must block")
	}
}

func TestTheLastRunCountsMetaFailuresOverRealSends(t *testing.T) {
	deps := launchDeps()
	deps.Counts = launchCounts{counts: wce.StatusCounts{Total: 300, Pending: 100, Sent: 100, Failed: 100}}
	deps.Failures = launchFailures{
		{Code: metaerror.Undeliverable, Count: 20},
		{Code: campaign.SkipOptedOut.FailureCode(), Count: 80},
	}

	report := runCheck(t, deps)

	if !found(report, launchcheck.Confirm, launchcheck.CodeLastRunFailures) {
		t.Fatalf("20 Meta failures over 120 real attempts is above 15%%: %+v", report.Findings)
	}
	if !found(report, launchcheck.Advice, launchcheck.CodeLastRunUndeliverable) {
		t.Fatal("undeliverable numbers are advised")
	}
}

func TestVozkosOwnSkipsAreNotTemplateFailures(t *testing.T) {
	deps := launchDeps()
	deps.Counts = launchCounts{counts: wce.StatusCounts{Total: 300, Pending: 100, Sent: 100, Failed: 100}}
	deps.Failures = launchFailures{{Code: campaign.SkipOptedOut.FailureCode(), Count: 100}}

	if found(runCheck(t, deps), launchcheck.Confirm, launchcheck.CodeLastRunFailures) {
		t.Fatal("contacts Vozko skipped are not the template's failures")
	}
}

func TestABalanceThatCannotPayForTheListIsConfirmed(t *testing.T) {
	deps := launchDeps()
	deps.Balances = launchBalances{micros: 50_000}

	if !found(runCheck(t, deps), launchcheck.Confirm, launchcheck.CodeBalanceShort) {
		t.Fatal("a short balance must be confirmed")
	}
}

func TestATemplateWithoutAnExitIsConfirmed(t *testing.T) {
	deps := launchDeps()
	deps.Exits = launchExits{configured: false}

	if !found(runCheck(t, deps), launchcheck.Confirm, launchcheck.CodeNoExit) {
		t.Fatal("no exit must be confirmed")
	}
}

func TestAMissingCampaignIsAnError(t *testing.T) {
	if _, err := NewLaunchCheck(launchDeps()).Run(context.Background(), "nope"); !errors.Is(err, wc.ErrCampaignNotFound) {
		t.Fatalf("err = %v", err)
	}
}
