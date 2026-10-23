package emailsending

import (
	"context"
	"errors"
	"testing"

	es "vozko/domain/emailsending"
)

func recordStatuses(d *es.SendingDomain) map[es.RecordPurpose]es.RecordStatus {
	out := map[es.RecordPurpose]es.RecordStatus{}
	for _, r := range d.Records {
		out[r.Purpose] = r.Status
	}
	return out
}

func TestRecheckStoresTheStatusOfEachRecord(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.provision(t, wsID)
	d, err := h.m.c().domainOfKind(ctx, wsID, es.DomainSharedProbation)
	if err != nil {
		t.Fatal(err)
	}
	h.provider.states[d.Domain] = es.IdentityState{DKIMVerified: true}
	h.records.mx[d.ReplyDomain] = []string{"inbound.test."}
	got, err := h.m.Domains.Recheck(ctx, wsID, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := map[es.RecordPurpose]es.RecordStatus{es.RecordDKIM: es.RecordVerified, es.RecordMailFromMX: es.RecordPending, es.RecordMailFromSPF: es.RecordPending, es.RecordReplyMX: es.RecordVerified}
	stored, _ := h.domains.Get(ctx, wsID, d.ID)
	for purpose, status := range want {
		if recordStatuses(got)[purpose] != status || recordStatuses(stored)[purpose] != status {
			t.Errorf("%s = %q (stored %q), want %q", purpose, recordStatuses(got)[purpose], recordStatuses(stored)[purpose], status)
		}
	}
}

func TestRecheckMarksRecordsUnknownWhenTheLookupFails(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	d := h.provisionVerified(t, wsID)
	h.records.err = errors.New("resolver down")
	got, err := h.m.Domains.Recheck(ctx, wsID, d.ID)
	if err != nil {
		t.Fatalf("a failed lookup must not fail the recheck: %v", err)
	}
	if s := recordStatuses(got)[es.RecordReplyMX]; s != es.RecordUnknown {
		t.Fatalf("reply mx = %q", s)
	}
	if s := recordStatuses(got)[es.RecordDKIM]; s != es.RecordVerified {
		t.Fatalf("dkim = %q", s)
	}
}

func TestVerificationSweepStoresRecordStatuses(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.provision(t, wsID)
	d, _ := h.m.c().domainOfKind(ctx, wsID, es.DomainSharedProbation)
	h.provider.states[d.Domain] = es.IdentityState{MailFromVerified: true}
	if err := h.m.Verification.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	stored, _ := h.domains.Get(ctx, wsID, d.ID)
	if s := recordStatuses(stored)[es.RecordMailFromMX]; s != es.RecordVerified {
		t.Fatalf("mail from mx = %q", s)
	}
	if s := recordStatuses(stored)[es.RecordReplyMX]; s != es.RecordPending {
		t.Fatalf("reply mx with no answer = %q", s)
	}
}

func TestSetupShowsThePromotionTargets(t *testing.T) {
	h := newHarness(t)
	view, err := h.m.Setup.Provision(context.Background(), wsID, "")
	if err != nil {
		t.Fatal(err)
	}
	if view.Probation == nil || view.Probation.Targets != es.CurrentPromotionTargets() {
		t.Fatalf("probation = %+v", view.Probation)
	}
}

func TestAdminListCarriesTheWorkspaceName(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.provision(t, wsID)
	views, _, err := h.m.Admin.List(ctx, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 1 || views[0].WorkspaceName != wsName {
		t.Fatalf("views = %+v", views)
	}
}

func TestSuppressionSearchIsALowerCasePrefix(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	if _, _, err := h.m.Suppressions.List(ctx, wsID, "  Ana.Souza@ ", 20, 10); err != nil {
		t.Fatal(err)
	}
	if h.suppressions.lastQuery != (es.SuppressionQuery{Prefix: "ana.souza@", Offset: 20, Limit: 10}) {
		t.Fatalf("query = %+v", h.suppressions.lastQuery)
	}
}
