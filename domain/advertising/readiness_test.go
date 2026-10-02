package advertising

import (
	"slices"
	"testing"
)

func allObserved() ReadinessFacts {
	return ReadinessFacts{
		Page:          Observed(true),
		AudienceTerms: Observed(true),
		Pixel:         Observed(true),
	}
}

func itemOf(t *testing.T, r AccountReadiness, key ReadinessKey) ReadinessItem {
	t.Helper()
	for _, item := range r.Items {
		if item.Key == key {
			return item
		}
	}
	t.Fatalf("no %s item in %v", key, r.Items)
	return ReadinessItem{}
}

func keysOf(items []ReadinessItem) []ReadinessKey {
	keys := make([]ReadinessKey, 0, len(items))
	for _, item := range items {
		keys = append(keys, item.Key)
	}
	return keys
}

func TestReadyAccountHasEveryReadableItemReadyAndNothingBlocks(t *testing.T) {
	r := BuildReadiness(spendableAccount(), allObserved())
	if !r.CanPublish() {
		t.Fatalf("ready account blocked by %v", r.Blocking())
	}
	for _, item := range r.Items {
		if item.Key == ReadyPhone || item.Key == ReadyEmail {
			continue
		}
		if item.State != StateReady {
			t.Fatalf("%s is %s", item.Key, item.State)
		}
		if item.Action != (ReadinessAction{}) {
			t.Fatalf("%s offers an action while ready: %+v", item.Key, item.Action)
		}
	}
}

func TestChecklistKeepsMetaOrder(t *testing.T) {
	want := []ReadinessKey{
		ReadyConnection, ReadyRole, ReadyAccountStatus, ReadyAccountDetails, ReadyPaymentMethod,
		ReadyPage, ReadyPhone, ReadyEmail, ReadyAudienceTerms, ReadyPixel,
	}
	if got := keysOf(BuildReadiness(spendableAccount(), allObserved()).Items); !slices.Equal(got, want) {
		t.Fatalf("order %v, want %v", got, want)
	}
}

func TestAccountItemsAreTheSpendRules(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*AdAccount)
		key    ReadinessKey
	}{
		{"needs reconnect", func(a *AdAccount) { a.Connection = ConnectionNeedsReconnect }, ReadyConnection},
		{"read only", func(a *AdAccount) { a.Tasks = []string{"ANALYZE"} }, ReadyRole},
		{"disabled", func(a *AdAccount) { a.MetaStatus = MetaAccountDisabled }, ReadyAccountStatus},
		{"status never read", func(a *AdAccount) { a.MetaStatus = 0 }, ReadyAccountStatus},
		{"unknown currency", func(a *AdAccount) { a.Currency = "" }, ReadyAccountDetails},
		{"unknown timezone", func(a *AdAccount) { a.Timezone = "Mars/Base" }, ReadyAccountDetails},
		{"no payment method", func(a *AdAccount) { a.HasFunding = false }, ReadyPaymentMethod},
	}
	for _, c := range cases {
		a := spendableAccount()
		c.mutate(a)
		r := BuildReadiness(a, allObserved())
		if a.CanSpend() == nil {
			t.Fatalf("%s: account still spends", c.name)
		}
		if r.CanPublish() {
			t.Fatalf("%s: readiness lets the account publish", c.name)
		}
		if got := r.Blocking(); len(got) == 0 || got[0] != c.key {
			t.Fatalf("%s: first blocker %v, want %s", c.name, got, c.key)
		}
		if item := itemOf(t, r, c.key); item.State != StateMissing {
			t.Fatalf("%s: %s is %s", c.name, c.key, item.State)
		}
	}
}

func TestBrokenConnectionLeavesEverythingElseUnknown(t *testing.T) {
	a := spendableAccount()
	a.Connection = ConnectionNeedsReconnect
	r := BuildReadiness(a, ReadinessFacts{})
	if item := itemOf(t, r, ReadyConnection); item.Action.InApp != ActionReconnect {
		t.Fatalf("reconnect action %+v", item.Action)
	}
	for _, item := range r.Items[1:] {
		if item.State != StateUnknown {
			t.Fatalf("%s is %s behind a broken connection", item.Key, item.State)
		}
		if item.Key != ReadyPhone && item.Key != ReadyEmail && item.Action != (ReadinessAction{}) {
			t.Fatalf("%s offers %+v while unknown", item.Key, item.Action)
		}
	}
}

func TestUnknownIsNeverReady(t *testing.T) {
	facts := allObserved()
	facts.Page = Unobserved
	r := BuildReadiness(spendableAccount(), facts)
	if r.CanPublish() {
		t.Fatal("a page that could not be read let the account publish")
	}
	if item := itemOf(t, r, ReadyPage); item.State != StateUnknown {
		t.Fatalf("page is %s", item.State)
	}
}

func TestOptionalItemsNeverBlockPublishing(t *testing.T) {
	facts := allObserved()
	facts.AudienceTerms = Unobserved
	facts.Pixel = Observed(false)
	r := BuildReadiness(spendableAccount(), facts)
	if !r.CanPublish() {
		t.Fatalf("optional items blocked: %v", r.Blocking())
	}
	for _, key := range []ReadinessKey{ReadyPhone, ReadyEmail, ReadyAudienceTerms, ReadyPixel} {
		if itemOf(t, r, key).Required {
			t.Fatalf("%s is required", key)
		}
	}
	if itemOf(t, r, ReadyAudienceTerms).State != StateUnknown {
		t.Fatal("unread terms reported as known")
	}
}

func TestMissingItemsOfferTheRightAction(t *testing.T) {
	a := spendableAccount()
	a.HasFunding = false
	a.Currency = ""
	a.Tasks = []string{"ANALYZE"}
	r := BuildReadiness(a, ReadinessFacts{Page: Observed(false), AudienceTerms: Observed(false), Pixel: Observed(false)})
	cases := map[ReadinessKey]ReadinessAction{
		ReadyRole:           {Portal: BusinessSettingsPortalURL},
		ReadyAccountDetails: {InApp: ActionSync},
		ReadyPaymentMethod:  {Portal: BillingPortalURL},
		ReadyPhone:          {Portal: PhoneVerificationPortalURL},
		ReadyEmail:          {Portal: EmailVerificationPortalURL},
		ReadyPage:           {Portal: PageCreatePortalURL},
		ReadyAudienceTerms:  {Portal: CustomAudienceTermsURL(a.MetaAccountID)},
		ReadyPixel:          {InApp: ActionCreatePixel},
	}
	for key, want := range cases {
		if got := itemOf(t, r, key).Action; got != want {
			t.Fatalf("%s action %+v, want %+v", key, got, want)
		}
	}
}

func TestTermsLinkIsScopedToTheAccount(t *testing.T) {
	if got := CustomAudienceTermsURL("act_42"); got != "https://business.facebook.com/ads/manage/customaudiences/tos/?act=42" {
		t.Fatalf("terms %s", got)
	}
}

func TestPhoneAndEmailAreNeverReadSoTheyStayUnknownWithoutBlocking(t *testing.T) {
	r := BuildReadiness(spendableAccount(), allObserved())
	for _, key := range []ReadinessKey{ReadyPhone, ReadyEmail} {
		if got := itemOf(t, r, key); got.State != StateUnknown || got.Required || got.Action.Portal == "" {
			t.Fatalf("%s: %+v", key, got)
		}
	}
}

func TestStatusActionFollowsTheReason(t *testing.T) {
	a := spendableAccount()
	a.MetaStatus = MetaAccountUnsettled
	if got := itemOf(t, BuildReadiness(a, allObserved()), ReadyAccountStatus).Action; got.Portal != BillingPortalURL {
		t.Fatalf("unsettled action %+v", got)
	}
	a.MetaStatus = MetaAccountDisabled
	if got := itemOf(t, BuildReadiness(a, allObserved()), ReadyAccountStatus).Action; got.Portal != AccountReviewPortalURL {
		t.Fatalf("disabled action %+v", got)
	}
}
