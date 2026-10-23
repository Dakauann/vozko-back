package emailsending

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	es "vozko/domain/emailsending"
	"vozko/domain/notification"
)

const day = 24 * time.Hour

func (h *harness) activatedDaysAgo(t *testing.T, ws string, days int) *es.SenderAccount {
	t.Helper()
	h.provisionVerified(t, ws)
	acct, err := h.accounts.GetByWorkspace(context.Background(), ws)
	if err != nil {
		t.Fatal(err)
	}
	acct.CreatedAt = h.now.Add(-time.Duration(days) * day)
	h.accounts.rows[acct.ID] = *acct
	return acct
}

func (h *harness) sweepUnused(t *testing.T) error {
	t.Helper()
	return h.m.UnusedRelease.Sweep(context.Background(), h.now)
}

func (h *harness) unusedNotices(ws string) []notification.Draft {
	h.alerts.mu.Lock()
	defer h.alerts.mu.Unlock()
	var out []notification.Draft
	for _, d := range h.alerts.drafts {
		if d.EpisodeKey == unusedReleaseKey(ws) {
			out = append(out, d)
		}
	}
	return out
}

func (h *harness) resolvedKey(key string) bool {
	h.alerts.mu.Lock()
	defer h.alerts.mu.Unlock()
	for _, k := range h.alerts.resolved {
		if k == key {
			return true
		}
	}
	return false
}

func TestAFreshUnusedAccountIsLeftAlone(t *testing.T) {
	h := newHarness(t)
	h.activatedDaysAgo(t, wsID, 10)
	if err := h.sweepUnused(t); err != nil {
		t.Fatal(err)
	}
	acct, _ := h.accounts.GetByWorkspace(context.Background(), wsID)
	if acct.UnusedWarnedAt != nil || len(h.unusedNotices(wsID)) != 0 {
		t.Fatal("an account inside its first 23 days must not be warned")
	}
}

func TestAnUnusedAccountIsWarnedAWeekAheadOnTheSameEpisode(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.activatedDaysAgo(t, wsID, 23)
	removedBefore := len(h.provider.removedIdents)
	if err := h.sweepUnused(t); err != nil {
		t.Fatal(err)
	}
	acct, _ := h.accounts.GetByWorkspace(ctx, wsID)
	if acct.UnusedWarnedAt == nil || !acct.UnusedWarnedAt.Equal(h.now) {
		t.Fatalf("warned at = %v", acct.UnusedWarnedAt)
	}
	notices := h.unusedNotices(wsID)
	if len(notices) != 1 {
		t.Fatalf("notices = %d", len(notices))
	}
	releaseDay := es.UnusedReleaseDay(acct.CreatedAt.Add(30 * day))
	n := notices[0]
	if !strings.Contains(n.Title, releaseDay) || !strings.Contains(n.Body, releaseDay) || !strings.Contains(n.Body, "contato@") ||
		n.Severity != notification.SeverityWarning || n.ActionURL != EmailOverviewPath || !n.Email {
		t.Fatalf("notice = %+v", n)
	}

	h.now = h.now.Add(day)
	if err := h.sweepUnused(t); err != nil {
		t.Fatal(err)
	}
	again, _ := h.accounts.GetByWorkspace(ctx, wsID)
	if !again.UnusedWarnedAt.Equal(*acct.UnusedWarnedAt) || len(h.unusedNotices(wsID)) != 2 {
		t.Fatalf("the next day republishes the same episode and keeps the stamp: %v %d", again.UnusedWarnedAt, len(h.unusedNotices(wsID)))
	}
	if len(h.provider.removedIdents) != removedBefore || len(h.tenants.retired) != 0 || len(h.unused.forgotten) != 0 {
		t.Fatal("a warning must not touch the provider")
	}
}

func TestAnUnusedAccountIsTornDownAtTheDeadlineAndItsNameIsFree(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	acct := h.activatedDaysAgo(t, wsID, 23)
	domain, err := h.m.c().domainOfKind(ctx, wsID, es.DomainSharedProbation)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.sweepUnused(t); err != nil {
		t.Fatal(err)
	}
	h.now = h.now.Add(6 * day)
	if err := h.sweepUnused(t); err != nil {
		t.Fatal(err)
	}
	if len(h.unused.forgotten) != 0 {
		t.Fatal("torn down before the deadline")
	}
	h.now = h.now.Add(day)
	if err := h.sweepUnused(t); err != nil {
		t.Fatal(err)
	}

	if len(h.routes.removed) == 0 || h.routes.removed[len(h.routes.removed)-1] != domain.ReplyDomain {
		t.Fatalf("reply route removed = %v", h.routes.removed)
	}
	if deleted := h.dns.deleted["envios.test"]; len(deleted) != len(domain.Records) || len(deleted) == 0 {
		t.Fatalf("dns deleted = %+v, want the %d shared records", deleted, len(domain.Records))
	}
	if h.provider.removedIdents[len(h.provider.removedIdents)-1] != domain.Domain {
		t.Fatalf("identities removed = %v", h.provider.removedIdents)
	}
	if lanes := h.provider.lanesOf("remove identity"); len(lanes) == 0 || lanes[len(lanes)-1] != es.LaneBackground {
		t.Fatalf("identity removal lanes = %v, want the background lane", lanes)
	}
	if len(h.tenants.retired) != 1 || h.tenants.retired[0].Name != acct.TenantName || len(h.tenants.retired[0].Identities) != 0 {
		t.Fatalf("retired = %+v", h.tenants.retired)
	}
	if len(h.unused.forgotten) != 1 || h.unused.forgotten[0] != wsID {
		t.Fatalf("forgotten = %v", h.unused.forgotten)
	}
	if !h.resolvedKey(unusedReleaseKey(wsID)) {
		t.Fatal("the warning episode must close")
	}
	view, err := h.m.Setup.Status(ctx, wsID)
	if err != nil || view.Provisioned {
		t.Fatalf("after teardown the workspace sees the activation card again: %+v %v", view, err)
	}
	if taken, _ := h.accounts.SlugTaken(ctx, acct.Slug); taken {
		t.Fatal("the released name is still taken")
	}
	if _, err := h.m.Setup.Provision(ctx, "22222222-2222-2222-2222-222222222222", acct.Slug); err != nil {
		t.Fatalf("another workspace takes the freed name: %v", err)
	}
}

func TestAnAccountThatSendsBeforeTheDeadlineIsKeptAndTheWarningCloses(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.activatedDaysAgo(t, wsID, 25)
	if err := h.sweepUnused(t); err != nil {
		t.Fatal(err)
	}
	h.unused.used[wsID] = true
	h.now = h.now.Add(10 * day)
	if err := h.sweepUnused(t); err != nil {
		t.Fatal(err)
	}
	acct, err := h.accounts.GetByWorkspace(ctx, wsID)
	if err != nil || acct.UnusedWarnedAt != nil {
		t.Fatalf("account = %+v %v, want kept with the stamp cleared", acct, err)
	}
	if !h.resolvedKey(unusedReleaseKey(wsID)) || len(h.unused.forgotten) != 0 || len(h.tenants.retired) != 0 {
		t.Fatal("a used account must be kept and its warning closed")
	}
}

func TestAProviderFailureKeepsTheAccountForTheNextSweep(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	acct := h.activatedDaysAgo(t, wsID, 40)
	stamp := h.now.Add(-8 * day)
	acct.UnusedWarnedAt = &stamp
	h.accounts.rows[acct.ID] = *acct
	h.routes.removeErr = errors.New("ses throttled")
	if err := h.sweepUnused(t); err == nil {
		t.Fatal("a failed teardown must fail the sweep")
	}
	if len(h.unused.forgotten) != 0 || len(h.tenants.retired) != 0 {
		t.Fatal("the account was forgotten with its provider setup in place")
	}
	if len(h.ops.alerts) == 0 {
		t.Fatal("a failed teardown alerts ops")
	}
	domains, _ := h.domains.ListByWorkspace(ctx, wsID)
	if len(domains) != 1 || !domains[0].AwaitsRelease() {
		t.Fatalf("the domain must wait in the release path: %+v", domains)
	}
	h.routes.removeErr = nil
	if err := h.sweepUnused(t); err != nil {
		t.Fatal(err)
	}
	if len(h.unused.forgotten) != 1 {
		t.Fatal("the next sweep finishes the teardown")
	}
}

func TestAnAccountUsedDuringTheTeardownIsRebuilt(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	acct := h.activatedDaysAgo(t, wsID, 40)
	stamp := h.now.Add(-8 * day)
	acct.UnusedWarnedAt = &stamp
	h.accounts.rows[acct.ID] = *acct
	h.unused.useOnForget[wsID] = true
	h.wake.take()
	if err := h.sweepUnused(t); err != nil {
		t.Fatalf("a kept account is not a failure: %v", err)
	}
	kept, err := h.accounts.GetByWorkspace(ctx, wsID)
	if err != nil || kept.Status != es.AccountDormant || kept.UnusedWarnedAt != nil {
		t.Fatalf("account = %+v %v, want dormant so the setup run rebuilds the tenant", kept, err)
	}
	domains, _ := h.domains.ListByWorkspace(ctx, wsID)
	if len(domains) != 1 || domains[0].Retired || domains[0].ReleaseState != es.ReleaseNone || !domains[0].AwaitsSetup() {
		t.Fatalf("domain = %+v, want reinstated and waiting for setup", domains)
	}
	if queued := h.wake.take(); len(queued) != 1 || queued[0] != wsID {
		t.Fatalf("setup runs queued = %v", queued)
	}
	if len(h.ops.alerts) == 0 {
		t.Fatal("the rebuild alerts ops")
	}
}

func TestTheSweepWalksEveryCandidateInBoundedPages(t *testing.T) {
	h := newHarness(t)
	for i := range unusedBatchSize + 5 {
		ws := fmt.Sprintf("%08d-0000-0000-0000-000000000000", i)
		h.accounts.rows[ws] = es.SenderAccount{ID: ws, WorkspaceID: ws, TenantName: "ws-" + ws, Mode: es.ModeProbation, Status: es.AccountActive,
			Slug: fmt.Sprintf("loja%d", i), CreatedAt: h.now.Add(-24 * day)}
	}
	if err := h.sweepUnused(t); err != nil {
		t.Fatal(err)
	}
	warned := 0
	for _, a := range h.accounts.rows {
		if a.UnusedWarnedAt != nil {
			warned++
		}
	}
	if warned != unusedBatchSize+5 || h.unused.pages != 2 {
		t.Fatalf("warned %d in %d pages", warned, h.unused.pages)
	}
}

func TestAnUnreadableCandidateListFailsTheSweep(t *testing.T) {
	h := newHarness(t)
	h.unused.listErr = errors.New("database down")
	if err := h.sweepUnused(t); err == nil {
		t.Fatal("the sweep must fail closed")
	}
}
