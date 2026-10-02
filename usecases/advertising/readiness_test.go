package advertising

import (
	"context"
	"errors"
	"testing"

	ads "vozko/domain/advertising"
)

func (w *world) readiness() *ReadinessUseCase { return NewReadinessUseCase(w.sync, w.gateway) }

func stateOf(t *testing.T, r *Readiness, key ads.ReadinessKey) ads.ReadinessState {
	t.Helper()
	for _, item := range r.Checklist.Items {
		if item.Key == key {
			return item.State
		}
	}
	t.Fatalf("no %s item", key)
	return ""
}

func TestReadinessOfAFundedAccountWithAPageIsReady(t *testing.T) {
	w := newWorld()
	w.gateway.pixels = []ads.Pixel{{MetaID: "px-1", Name: "Site"}}
	r, err := w.readiness().Readiness(context.Background(), "ws-1", "acc-1")
	if err != nil {
		t.Fatal(err)
	}
	if !r.Checklist.CanPublish() {
		t.Fatalf("blocked by %v", r.Checklist.Blocking())
	}
	for _, key := range []ads.ReadinessKey{ads.ReadyPage, ads.ReadyAudienceTerms, ads.ReadyPixel} {
		if got := stateOf(t, r, key); got != ads.StateReady {
			t.Fatalf("%s is %s", key, got)
		}
	}
}

func TestReadinessReportsWhatTheProfileStillLacks(t *testing.T) {
	w := newWorld()
	w.accounts.byID["acc-1"].HasFunding = false
	w.gateway.pages = []ads.RemotePage{{PageID: "page-1", Name: "Loja", CanAdvertise: false}}
	w.gateway.termsOK = false
	r, err := w.readiness().Readiness(context.Background(), "ws-1", "acc-1")
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[ads.ReadinessKey]ads.ReadinessState{
		ads.ReadyPaymentMethod: ads.StateMissing,
		ads.ReadyPage:          ads.StateMissing,
		ads.ReadyAudienceTerms: ads.StateMissing,
		ads.ReadyPixel:         ads.StateMissing,
	} {
		if got := stateOf(t, r, key); got != want {
			t.Fatalf("%s is %s, want %s", key, got, want)
		}
	}
}

func TestAFailedReadIsUnknownAndBlocks(t *testing.T) {
	w := newWorld()
	w.gateway.failOn = "list_pages"
	w.gateway.failWith = errors.New("meta down")
	r, err := w.readiness().Readiness(context.Background(), "ws-1", "acc-1")
	if err != nil {
		t.Fatal(err)
	}
	if got := stateOf(t, r, ads.ReadyPage); got != ads.StateUnknown {
		t.Fatalf("page is %s", got)
	}
	if r.Checklist.CanPublish() {
		t.Fatal("unknown page let the account publish")
	}
}

func TestReadinessOfADisconnectedAccountAsksToReconnectWithoutCallingMeta(t *testing.T) {
	w := newWorld()
	w.accounts.byID["acc-1"].Connection = ads.ConnectionNeedsReconnect
	r, err := w.readiness().Readiness(context.Background(), "ws-1", "acc-1")
	if err != nil {
		t.Fatal(err)
	}
	if got := stateOf(t, r, ads.ReadyConnection); got != ads.StateMissing {
		t.Fatalf("connection is %s", got)
	}
	if len(w.gateway.calls) != 0 {
		t.Fatalf("called meta: %v", w.gateway.calls)
	}
}

func TestReadinessOfAnotherWorkspacesAccountIsNotFound(t *testing.T) {
	w := newWorld()
	if _, err := w.readiness().Readiness(context.Background(), "ws-2", "acc-1"); !errors.Is(err, ads.ErrAccountNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestAnAccountThatIsNotReadyStillValidatesTheDraftButNeverPublishes(t *testing.T) {
	w := newWorld()
	w.accounts.byID["acc-1"].HasFunding = false
	if _, err := w.publisher().Check(context.Background(), "ws-1", publishableDraft()); err != nil {
		t.Fatalf("draft check refused: %v", err)
	}
	if _, err := w.publisher().Preflight(context.Background(), "ws-1", publishableDraft()); !errors.Is(err, ads.ErrNoFundingSource) {
		t.Fatalf("preflight got %v", err)
	}
	if _, err := w.publisher().Publish(context.Background(), PublishInput{WorkspaceID: "ws-1", Draft: publishableDraft()}); !errors.Is(err, ads.ErrNoFundingSource) {
		t.Fatalf("publish got %v", err)
	}
	if len(w.jobs.byID) != 0 || len(w.fees.charged) != 0 {
		t.Fatal("a job or a fee was created for an account that cannot spend")
	}
}

func TestBillingShowsThePaymentMethodOnlyToAdmins(t *testing.T) {
	w := newWorld()
	w.gateway.billing = ads.RemoteBilling{PaymentMethod: "Visa *1234", Balance: 1500, Prepay: true}
	r, err := w.readiness().Readiness(context.Background(), "ws-1", "acc-1")
	if err != nil {
		t.Fatal(err)
	}
	if r.Billing == nil || r.Billing.PaymentMethod != "Visa *1234" || r.Billing.Balance != 1500 {
		t.Fatalf("admin billing %+v", r.Billing)
	}
	w.accounts.byID["acc-1"].Tasks = []string{"ADVERTISE", "ANALYZE"}
	r, err = w.readiness().Readiness(context.Background(), "ws-1", "acc-1")
	if err != nil {
		t.Fatal(err)
	}
	if r.Billing == nil || r.Billing.PaymentMethod != "" {
		t.Fatalf("advertiser billing %+v", r.Billing)
	}
	if w.gateway.billingWith[1] {
		t.Fatal("asked meta for the payment method without the MANAGE task")
	}
}

func TestReadOnlyProfileGetsNoBillingSummary(t *testing.T) {
	w := newWorld()
	w.accounts.byID["acc-1"].Tasks = []string{"ANALYZE"}
	r, err := w.readiness().Readiness(context.Background(), "ws-1", "acc-1")
	if err != nil {
		t.Fatal(err)
	}
	if r.Billing != nil || len(w.gateway.billingWith) != 0 {
		t.Fatalf("read only profile read billing: %+v", r.Billing)
	}
}
