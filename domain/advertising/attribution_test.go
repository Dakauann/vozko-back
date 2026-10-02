package advertising

import "testing"

func TestOutcomeDividesSpendByOurOwnConversationsAndLeads(t *testing.T) {
	spend := Metrics{Currency: "BRL", SpendMicros: 100_000_000}
	out := OutcomeOf(spend, Attribution{Conversations: 20, Leads: 5, WonDeals: 2, Revenue: 50_000, RevenueCurrency: "BRL"})
	if *out.CostPerConversation != 5_000_000 || *out.CostPerLead != 20_000_000 {
		t.Fatalf("costs %d %d", *out.CostPerConversation, *out.CostPerLead)
	}
	if out.RevenueMicros != 500_000_000 || *out.ROAS != 5 {
		t.Fatalf("revenue %d roas %v", out.RevenueMicros, *out.ROAS)
	}
}

func TestRevenueInAnotherCurrencyGivesNoROAS(t *testing.T) {
	out := OutcomeOf(Metrics{Currency: "USD", SpendMicros: 1_000_000}, Attribution{WonDeals: 1, Revenue: 100, RevenueCurrency: "BRL"})
	if out.ROAS != nil || out.RevenueMicros != 0 {
		t.Fatalf("mixed currencies produced roas %+v", out)
	}
}

func TestNoConversationsMeansNoCostRatherThanZero(t *testing.T) {
	out := OutcomeOf(Metrics{Currency: "BRL", SpendMicros: 1_000_000}, Attribution{})
	if out.CostPerConversation != nil || out.CostPerLead != nil || out.ROAS != nil {
		t.Fatalf("invented costs %+v", out)
	}
}

func TestSummedRevenueInTwoCurrenciesIsMarkedMixed(t *testing.T) {
	total := SumAttributions([]Attribution{
		{Conversations: 1, WonDeals: 1, Revenue: 10, RevenueCurrency: "BRL"},
		{Conversations: 2, WonDeals: 1, Revenue: 10, RevenueCurrency: "USD"},
		{Conversations: 3},
	})
	if total.Conversations != 6 || total.RevenueCurrency != "MIXED" {
		t.Fatalf("total %+v", total)
	}
	if OutcomeOf(Metrics{Currency: "BRL", SpendMicros: 1}, total).ROAS != nil {
		t.Fatal("mixed revenue produced roas")
	}
}

func TestLeadCostIsTheDaySpendSharedByTheDayConversations(t *testing.T) {
	c := LeadCost{SpendMicros: 30_000_000, Conversations: 3}
	if *c.Estimate() != 10_000_000 {
		t.Fatalf("estimate %d", *c.Estimate())
	}
	if (LeadCost{SpendMicros: 1}).Estimate() != nil {
		t.Fatal("estimate without conversations")
	}
}

func TestNoSpendDataGivesNoCostEvenWithConversations(t *testing.T) {
	out := OutcomeOf(Metrics{}, Attribution{Conversations: 4, Leads: 2, WonDeals: 1, Revenue: 100, RevenueCurrency: "BRL"})
	if out.CostPerConversation != nil || out.CostPerLead != nil || out.ROAS != nil || out.Conversations != 4 {
		t.Fatalf("outcome %+v", out)
	}
}
