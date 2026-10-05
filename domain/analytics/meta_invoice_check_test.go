package analytics

import "testing"

func invoiceAccount() InvoiceAccount {
	return InvoiceAccount{WABAID: "1029", Name: "Clínica", Provider: "meta", AccessToken: "tok", TemplateSends: 1_000}
}

func TestMetasPointsSplitIntoTemplatesAndServiceChargedOrFree(t *testing.T) {
	got := SumMetaVolumes([]MetaVolumePoint{
		{Category: "UTILITY", PricingType: "REGULAR", Volume: 900},
		{Category: "MARKETING", PricingType: "REGULAR", Volume: 50},
		{Category: "UTILITY", PricingType: "FREE_CUSTOMER_SERVICE", Volume: 30},
		{Category: "SERVICE", PricingType: "FREE_CUSTOMER_SERVICE", Volume: 400},
		{Category: "SERVICE", PricingType: "REGULAR", Volume: 2},
	})
	want := MetaVolumes{Templates: 980, ChargedTemplates: 950, ChargedService: 2, FreeService: 400}
	if got != want {
		t.Fatalf("volumes %+v, want %+v", got, want)
	}
}

func TestOurTemplateSendsAreComparedWithMetasTemplates(t *testing.T) {
	check := CompareInvoice(invoiceAccount(), MetaVolumes{Templates: 1_036, ChargedTemplates: 1_000, FreeService: 400})
	if check.State != InvoiceMatched || check.OurTemplates != 1_000 || check.MetaTemplates != 1_036 || check.Difference != 36 {
		t.Fatalf("check %+v", check)
	}
	if check.MetaChargedTemplates != 1_000 || check.MetaFreeService != 400 {
		t.Fatalf("meta split %+v", check)
	}
	if check.DifferencePct == nil || *check.DifferencePct < 3.5 || *check.DifferencePct > 3.7 {
		t.Fatalf("pct %v", check.DifferencePct)
	}
}

func TestADifferencePastTheToleranceIsToCheck(t *testing.T) {
	if check := CompareInvoice(invoiceAccount(), MetaVolumes{Templates: 1_200}); check.State != InvoiceToCheck {
		t.Fatalf("check %+v", check)
	}
}

func TestOurSendsWithNothingFromMetaAreToCheck(t *testing.T) {
	if check := CompareInvoice(invoiceAccount(), MetaVolumes{}); check.State != InvoiceToCheck {
		t.Fatalf("an empty answer from Meta must not pass as a match: %+v", check)
	}
}

func TestServiceMessagesMetaChargedAreReportedWithoutChangingTheMatch(t *testing.T) {
	check := CompareInvoice(invoiceAccount(), MetaVolumes{Templates: 1_000, ChargedService: 3})
	if check.State != InvoiceMatched || check.MetaChargedService != 3 {
		t.Fatalf("check %+v", check)
	}
}

func TestNothingOnEitherSideMatches(t *testing.T) {
	account := invoiceAccount()
	account.TemplateSends = 0
	check := CompareInvoice(account, MetaVolumes{})
	if check.State != InvoiceMatched || check.DifferencePct != nil {
		t.Fatalf("check %+v", check)
	}
}

func TestTheInvoiceTotalsAddMetasServiceCountsAndUnreadAccounts(t *testing.T) {
	totals := SumInvoice([]WABAInvoiceCheck{
		{State: InvoiceMatched, MetaChargedService: 1, MetaFreeService: 400},
		{State: InvoiceToCheck, MetaChargedService: 2, MetaFreeService: 100},
		UnavailableInvoice(invoiceAccount(), ReasonNoToken),
	})
	if totals != (InvoiceTotals{MetaChargedService: 3, MetaFreeService: 500, UnavailableAccounts: 1, IdleAccounts: 0}) {
		t.Fatalf("totals %+v", totals)
	}
}

func TestAccountsMetaCannotBeAskedAboutAreUnavailable(t *testing.T) {
	dialog := invoiceAccount()
	dialog.Provider = "dialog360"
	if r, ok := dialog.Unreadable(); !ok || r != ReasonDialog360 {
		t.Fatalf("360dialog: %s %v", r, ok)
	}
	noToken := invoiceAccount()
	noToken.AccessToken = " "
	if r, ok := noToken.Unreadable(); !ok || r != ReasonNoToken {
		t.Fatalf("no token: %s %v", r, ok)
	}
	if _, ok := invoiceAccount().Unreadable(); ok {
		t.Fatal("a Meta account with a token can be read")
	}
}

func TestAnAccountIsActiveWithSendsOrServiceMessagesInThePeriod(t *testing.T) {
	cases := map[string]struct {
		account InvoiceAccount
		want    bool
	}{
		"template sends":        {InvoiceAccount{TemplateSends: 3}, true},
		"refunds only":          {InvoiceAccount{TemplateSends: -1}, true},
		"service messages":      {InvoiceAccount{ServiceMessages: 2}, true},
		"nothing in the period": {InvoiceAccount{}, false},
	}
	for name, tc := range cases {
		if got := tc.account.Active(); got != tc.want {
			t.Errorf("%s: %v, want %v", name, got, tc.want)
		}
	}
}

func TestSplittingKeepsActiveAccountsAndCountsTheIdleOnes(t *testing.T) {
	active, idle := SplitActive([]InvoiceAccount{{WABAID: "a", TemplateSends: 1}, {WABAID: "b"}, {WABAID: "c", ServiceMessages: 4}})
	if len(active) != 2 || active[0].WABAID != "a" || active[1].WABAID != "c" || idle != 1 {
		t.Fatalf("active %+v idle %d", active, idle)
	}
}
