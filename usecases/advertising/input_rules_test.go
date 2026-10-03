package advertising

import (
	"context"
	"slices"
	"testing"

	ads "vozko/domain/advertising"
)

func TestReportRefusesAnUnknownLevelAsAFieldIssue(t *testing.T) {
	w := newWorld()
	_, err := reporter(w).Report(context.Background(), ReportQuery{WorkspaceID: "ws-1", AccountID: "acc-1", Level: "account", Range: septemberLast()})
	requireIssue(t, err, "level", "invalid")
}

func TestReportWithoutLevelShowsCampaigns(t *testing.T) {
	w := newWorld()
	report, err := reporter(w).Report(context.Background(), ReportQuery{WorkspaceID: "ws-1", AccountID: "acc-1", Range: septemberLast()})
	if err != nil || report.Level != ads.LevelCampaign {
		t.Fatalf("report %+v err %v", report, err)
	}
}

func TestPostsOfAnUnknownPlatformAreRefusedBeforeMeta(t *testing.T) {
	w := newWorld()
	_, err := NewAssetsUseCase(w.sync, w.gateway, nil).Posts(context.Background(), "ws-1", "acc-1", "page-1", "threads")
	requireIssue(t, err, "platform", "invalid")
	if len(w.gateway.calls) != 0 {
		t.Fatalf("meta called %v", w.gateway.calls)
	}
}

func TestFormLeadsPagingIsBoundedAndDefaulted(t *testing.T) {
	_, uc, forms, _, _ := formsWorld()
	forms.byID["f-1"] = &ads.TrackedForm{MetaID: "f-1", WorkspaceID: "ws-1", AdAccountID: "acc-1", PageID: "page-1"}
	for _, c := range []struct {
		limit, offset int
		field         string
	}{{-1, 0, "limit"}, {ads.MaxLeadsPage + 1, 0, "limit"}, {10, -1, "offset"}, {10, ads.MaxLeadsOffset + 1, "offset"}} {
		_, _, err := uc.Leads(context.Background(), "ws-1", "f-1", c.limit, c.offset)
		requireIssue(t, err, c.field, "invalid")
	}
	if _, _, err := uc.Leads(context.Background(), "ws-1", "f-1", 0, 0); err != nil {
		t.Fatalf("default page refused: %v", err)
	}
}

func TestPreflightQuotesTheFeeForEveryAd(t *testing.T) {
	w := newWorld()
	draft := publishableDraft()
	draft.Ads = append(draft.Ads, ads.AdItem{Creative: imageAd()})
	draft.Campaign.Budget, draft.AdSet.Budget = nil, &ads.Budget{Kind: ads.BudgetDaily, Amount: 2000}
	pre, err := w.publisher().Preflight(context.Background(), "ws-1", draft)
	if err != nil {
		t.Fatal(err)
	}
	if pre.FeeTotal.PriceMicros != 2*pre.Fee.PriceMicros || pre.FeeTotal.Currency != pre.Fee.Currency {
		t.Fatalf("fee %+v total %+v", pre.Fee, pre.FeeTotal)
	}
	if slices.Contains(w.gateway.calls, "create_campaign") {
		t.Fatal("preflight wrote to meta")
	}
}
