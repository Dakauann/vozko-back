package analytics

import (
	"testing"

	"vozko/domain/shared"
	wsc "vozko/domain/workspace_config"
)

var rates = CostRates{USDToBRLMicros: 5_000_000}

func usd(m *Money) int64 {
	if m == nil {
		return -1
	}
	return m.USDMicros
}

func TestMoneyIsShownInReaisOnlyWithAnExchangeRate(t *testing.T) {
	got := rates.Money(2_000_000)
	if got.USDMicros != 2_000_000 || got.BRLMicros == nil || *got.BRLMicros != 10_000_000 {
		t.Fatalf("money %+v", got)
	}
	if (CostRates{}).Money(2_000_000).BRLMicros != nil {
		t.Fatal("without a rate there is no amount in reais to show")
	}
}

func TestMetasAnswerSplitsIntoChargedFreeAndUnanswered(t *testing.T) {
	totals := MetaServiceMessageCostTotals{ServiceMessages: 100, MetaAnswered: 74, MetaConfirmed: 52}
	if got := totals.SplitAnswers(); got != (MetaAnswers{Charged: 52, Free: 22, NoAnswer: 26}) {
		t.Fatalf("answers %+v", got)
	}
	if got := (MetaServiceMessageCostTotals{ServiceMessages: 3, MetaAnswered: 5, MetaConfirmed: 6}).SplitAnswers(); got.Free < 0 || got.NoAnswer < 0 {
		t.Fatalf("counts never go negative: %+v", got)
	}
}

func TestServiceCostIsMetasChargedCountTimesTheWorkspaceCost(t *testing.T) {
	costs := ServiceMessageCosts{"ws-1": 4_000}
	if got, ok := costs.Cost("ws-1", 100); !ok || got != 400_000 {
		t.Fatalf("cost %d %v", got, ok)
	}
	if got, ok := costs.Cost("ws-2", 0); !ok || got != 0 {
		t.Fatalf("nothing charged costs nothing, got %d %v", got, ok)
	}
	if _, ok := costs.Cost("ws-2", 5); ok {
		t.Fatal("charged messages without a cost are unknown, never zero")
	}
}

func TestVozkoPaysMetaSoTheMarginTakesTemplatesAndService(t *testing.T) {
	service := int64(400_000)
	e := NewWorkspaceEconomics(wsc.MetaPayerVozko, 1_000_000, 600_000, &service, rates)
	if usd(e.ServiceCost) != 400_000 || usd(e.VozkoMetaCost) != 1_000_000 || usd(e.RealMargin) != 0 || e.ServiceExceedsPrice {
		t.Fatalf("economics %+v", e)
	}
	over := int64(600_000)
	if e := NewWorkspaceEconomics(wsc.MetaPayerVozko, 1_000_000, 600_000, &over, rates); usd(e.RealMargin) != -200_000 || !e.ServiceExceedsPrice {
		t.Fatalf("service past what the price covers must be flagged: %+v", e)
	}
}

func TestAnUnknownServiceCostLeavesVozkosCostAndMarginUnknown(t *testing.T) {
	e := NewWorkspaceEconomics(wsc.MetaPayerVozko, 1_000_000, 600_000, nil, rates)
	if e.ServiceCost != nil || e.VozkoMetaCost != nil || e.RealMargin != nil || e.ServiceExceedsPrice {
		t.Fatalf("economics %+v", e)
	}
	if e.PaidByClient.USDMicros != 1_000_000 || e.TemplateCost.USDMicros != 600_000 {
		t.Fatalf("what is known is still shown: %+v", e)
	}
}

func TestAClientWhoPaysMetaLeavesTheWholePaymentAsMargin(t *testing.T) {
	e := NewWorkspaceEconomics(wsc.MetaPayerClient, 1_000_000, 600_000, nil, rates)
	if usd(e.VozkoMetaCost) != 0 || usd(e.RealMargin) != 1_000_000 || e.ServiceExceedsPrice || e.MetaPayer != "client" {
		t.Fatalf("economics %+v", e)
	}
}

func TestApplyingCostsFillsRowsAndTotalsFromTheSameRates(t *testing.T) {
	report := &MetaServiceMessageCostReport{
		Totals: MetaServiceMessageCostTotals{
			ServiceMessages: 250, MetaAnswered: 200, MetaConfirmed: 150,
			PaidMicros: 3_000_000, VozkoTemplateCostMicros: 1_000_000,
			ServiceCharges: []ServiceCharge{
				{WorkspaceID: "ws-1", MetaPayer: "vozko", Charged: 100},
				{WorkspaceID: "ws-2", MetaPayer: "client", Charged: 50},
			},
		},
		Workspaces: &shared.PaginatedResult[*WorkspaceMetaServiceMessageCost]{Items: []*WorkspaceMetaServiceMessageCost{
			{WorkspaceID: "ws-1", MetaConfirmed: 100, MetaPayer: "vozko", PaidMicros: 1_000_000, TemplateCostMicros: 600_000},
			{WorkspaceID: "ws-2", MetaConfirmed: 50, MetaPayer: "client", PaidMicros: 2_000_000, TemplateCostMicros: 400_000},
		}},
		Numbers: []*NumberMetaCost{{PhoneID: "p-1", Answered: 180, Charged: 150}},
	}
	report.ApplyRates(rates, ServiceMessageCosts{"ws-1": 5_000, "ws-2": 4_000})

	if report.Totals.Answers != (MetaAnswers{Charged: 150, Free: 50, NoAnswer: 50}) || report.Totals.ServiceCostMissing != 0 {
		t.Fatalf("totals %+v", report.Totals)
	}
	if usd(report.Totals.ConfirmedServiceCost) != 700_000 || usd(report.Totals.VozkoMetaCost) != 1_500_000 || usd(report.Totals.RealMargin) != 1_500_000 {
		t.Fatalf("service %+v vozko %+v margin %+v", report.Totals.ConfirmedServiceCost, report.Totals.VozkoMetaCost, report.Totals.RealMargin)
	}
	if usd(report.Workspaces.Items[0].Economics.RealMargin) != -100_000 || !report.Workspaces.Items[0].Economics.ServiceExceedsPrice {
		t.Fatalf("row %+v", report.Workspaces.Items[0].Economics)
	}
	if usd(report.Workspaces.Items[1].Economics.ServiceCost) != 200_000 || usd(report.Workspaces.Items[1].Economics.RealMargin) != 2_000_000 {
		t.Fatalf("row %+v", report.Workspaces.Items[1].Economics)
	}
	if report.Numbers[0].State != NumberCharging {
		t.Fatalf("number %+v", report.Numbers[0])
	}
}

func TestAWorkspaceWithoutACostLeavesTheTotalsUnknownAndIsCounted(t *testing.T) {
	report := &MetaServiceMessageCostReport{Totals: MetaServiceMessageCostTotals{
		PaidMicros: 3_000_000, VozkoTemplateCostMicros: 1_000_000,
		ServiceCharges: []ServiceCharge{
			{WorkspaceID: "ws-1", MetaPayer: "vozko", Charged: 100},
			{WorkspaceID: "ws-2", MetaPayer: "vozko", Charged: 10},
		},
	}}
	report.ApplyRates(rates, ServiceMessageCosts{"ws-1": 5_000})
	if report.Totals.ServiceCostMissing != 1 || report.Totals.ConfirmedServiceCost != nil || report.Totals.VozkoMetaCost != nil || report.Totals.RealMargin != nil {
		t.Fatalf("totals %+v", report.Totals)
	}
	if report.Totals.PaidByClients.USDMicros != 3_000_000 {
		t.Fatalf("what clients paid is still known: %+v", report.Totals.PaidByClients)
	}
}

func TestOnlyAClientPaidServiceWithoutACostStillKnowsVozkosSide(t *testing.T) {
	report := &MetaServiceMessageCostReport{Totals: MetaServiceMessageCostTotals{
		PaidMicros: 3_000_000, VozkoTemplateCostMicros: 1_000_000,
		ServiceCharges: []ServiceCharge{{WorkspaceID: "ws-2", MetaPayer: "client", Charged: 10}},
	}}
	report.ApplyRates(rates, ServiceMessageCosts{})
	if report.Totals.ConfirmedServiceCost != nil || usd(report.Totals.VozkoMetaCost) != 1_000_000 || usd(report.Totals.RealMargin) != 2_000_000 {
		t.Fatalf("totals %+v", report.Totals)
	}
}

func TestANumbersStateFollowsWhatMetaAnswered(t *testing.T) {
	cases := map[NumberState]NumberMetaCost{
		NumberCharging: {Answered: 10, Charged: 1},
		NumberFree:     {Answered: 10},
		NumberNoAnswer: {ServiceMessages: 10},
	}
	for want, n := range cases {
		if got := n.BillingState(); got != want {
			t.Errorf("%+v: %s, want %s", n, got, want)
		}
	}
}

func TestAttachedDetailsCarryTheUnlinkedTotal(t *testing.T) {
	report := &MetaServiceMessageCostReport{}
	report.AttachDetails(
		[]*NumberMetaCost{{PhoneID: "p-1"}},
		[]*UnlinkedNumber{{PhoneNumberID: "885", Messages: 27}, {PhoneNumberID: "", Messages: 3}},
	)
	if len(report.Numbers) != 1 || len(report.Unlinked) != 2 || report.Totals.UnlinkedServiceMessages != 30 {
		t.Fatalf("report %+v totals %+v", report, report.Totals)
	}
}

func TestChargedWorkspacesAreListedOnce(t *testing.T) {
	totals := MetaServiceMessageCostTotals{ServiceCharges: []ServiceCharge{{WorkspaceID: "ws-1", Charged: 1}, {WorkspaceID: "ws-2", Charged: 2}}}
	if got := totals.ChargedWorkspaceIDs(); len(got) != 2 || got[0] != "ws-1" || got[1] != "ws-2" {
		t.Fatalf("ids %v", got)
	}
}
