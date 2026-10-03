package advertisinghttp

import (
	"encoding/json"
	"strings"
	"testing"

	"vozko/domain/advertising"
	adsuc "vozko/usecases/advertising"
)

func presentableAccount(tasks ...string) *advertising.AdAccount {
	return &advertising.AdAccount{
		MetaAccountID: "111", Currency: "BRL", Timezone: "America/Sao_Paulo", MetaStatus: advertising.MetaAccountActive,
		HasFunding: true, Connection: advertising.ConnectionConnected, Tasks: tasks,
	}
}

func TestAccountResponseCarriesTheRoleAndWhatItAllows(t *testing.T) {
	cases := []struct {
		tasks     []string
		role      string
		canManage bool
		canCap    bool
	}{
		{[]string{"MANAGE", "ADVERTISE"}, "admin", true, true},
		{[]string{"ADVERTISE"}, "advertiser", true, false},
		{[]string{"ANALYZE"}, "read_only", false, false},
		{nil, "read_only", false, false},
	}
	for _, c := range cases {
		got := presentAccount(presentableAccount(c.tasks...))
		if got.Role != c.role || got.CanManage != c.canManage || got.CanSetSpendCap != c.canCap {
			t.Fatalf("%v: %+v", c.tasks, got)
		}
		if !c.canManage && (got.CanSpend || got.SpendBlocker != "account_read_only") {
			t.Fatalf("%v: read only account offered writes %+v", c.tasks, got)
		}
	}
}

func TestBudgetMinimumIsPresentedWithItsField(t *testing.T) {
	got := presentBudgetMinimum(&advertising.BudgetMinimum{Field: "adSet.budget.amount", Daily: 519, Currency: "BRL"})
	if got == nil || *got != (BudgetMinimumResponse{Field: "adSet.budget.amount", Daily: 519, Currency: "BRL"}) {
		t.Fatalf("got %+v", got)
	}
	if presentBudgetMinimum(nil) != nil {
		t.Fatal("absent minimum presented")
	}
}

func TestReportDefinitionsAlwaysSendLists(t *testing.T) {
	raw, err := json.Marshal(presentDefinition(advertising.ReportDefinition{View: advertising.ViewPivot}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"breakdowns":[]`) || !strings.Contains(string(raw), `"metrics":[]`) {
		t.Fatalf("got %s", raw)
	}
}

func TestARunTellsTheKindOfEveryMetricAndNeverSendsNull(t *testing.T) {
	run := presentRun(&adsuc.ReportRun{Currency: "BRL", Table: advertising.ReportTable{View: advertising.ViewTrend, Metrics: []advertising.ReportMetric{advertising.ReportSpend, advertising.ReportCTR}}})
	raw, err := json.Marshal(run)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"metricKinds":{"ctr":"percent","spend":"money"}`, `"rows":[]`, `"series":[]`, `"totals":{}`, `"breakdowns":[]`} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("missing %s in %s", want, raw)
		}
	}
}
