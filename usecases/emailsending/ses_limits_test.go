package emailsending

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	es "vozko/domain/emailsending"
)

func TestTheQuotaRefreshStoresTheAccountReadForThePacer(t *testing.T) {
	h := newHarness(t)
	if err := h.m.SendQuota.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	stored, err := loadSendQuota(h.shared, h.now)
	if err != nil {
		t.Fatal(err)
	}
	if !stored.Read || stored.MaxSendRate != 14 || stored.Max24HourSend != 50000 || !stored.ReadAt.Equal(h.now) {
		t.Fatalf("stored = %+v", stored)
	}
	if len(h.ops.alerts) != 0 {
		t.Fatalf("a quiet quota raises nothing: %v", h.ops.alerts)
	}
}

func TestTheQuotaRefreshAlertsOnceAtEightyPercentAndAgainAfterItRecovers(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.quota.quota.SentLast24Hours = 40000
	for range 3 {
		if err := h.m.SendQuota.Execute(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if len(h.ops.alerts) != 1 || !strings.Contains(h.ops.alerts[0], "40000 de 50000") {
		t.Fatalf("alerts = %v, want one alert with the exact numbers", h.ops.alerts)
	}
	h.quota.quota.SentLast24Hours = 100
	if err := h.m.SendQuota.Execute(ctx); err != nil {
		t.Fatal(err)
	}
	h.quota.quota.SentLast24Hours = 45000
	if err := h.m.SendQuota.Execute(ctx); err != nil {
		t.Fatal(err)
	}
	if len(h.ops.alerts) != 2 {
		t.Fatalf("alerts = %v, want a new alert for the new episode", h.ops.alerts)
	}
}

func TestAFailedQuotaReadKeepsTheLastReadAndReports(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	if err := h.m.SendQuota.Execute(ctx); err != nil {
		t.Fatal(err)
	}
	h.quota.err = errors.New("TooManyRequestsException")
	h.now = h.now.Add(time.Minute)
	for range 2 {
		if err := h.m.SendQuota.Execute(ctx); !errors.Is(err, ErrProviderUnavailable) {
			t.Fatalf("err = %v", err)
		}
	}
	stored, _ := loadSendQuota(h.shared, h.now)
	if !stored.ReadAt.Equal(testNow) {
		t.Fatalf("the last good read must stay, got %+v", stored)
	}
	if len(h.ops.alerts) != 1 {
		t.Fatalf("alerts = %v, want one alert per episode", h.ops.alerts)
	}
}

func TestAdminSendQuotaShowsTheProviderQuotaWithOurSendsSinceTheRead(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	unread, err := h.m.Admin.SendQuota(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if unread.Quota.Read || unread.Quota.TokensPerSecond() != es.SESSandboxMaxSendRate {
		t.Fatalf("before any read the sandbox quota is shown, got %+v", unread)
	}
	h.quota.quota.SentLast24Hours = 39990
	if err := h.m.SendQuota.Execute(ctx); err != nil {
		t.Fatal(err)
	}
	for range 10 {
		if err := countSent(h.shared, h.now); err != nil {
			t.Fatal(err)
		}
	}
	view, err := h.m.Admin.SendQuota(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if view.SentSinceRead != 10 || view.Used24Hours != 40000 || view.UsageBasisPoints != 8000 || view.PriorityReserve != 2500 || !view.Fresh {
		t.Fatalf("view = %+v", view)
	}
}

func TestTheCapacityCheckAlertsWithTheExactNumbersAtEightyPercent(t *testing.T) {
	h := newHarness(t)
	h.provisionVerified(t, wsID)
	h.routes.ipFilters = 85
	if err := h.m.Capacity.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(h.ops.alerts) != 1 || !strings.Contains(h.ops.alerts[0], "85 de 100") || !strings.Contains(h.ops.alerts[0], string(es.ResourceIPFilters)) {
		t.Fatalf("alerts = %v", h.ops.alerts)
	}
	if got := h.routes.lanesOf("filters"); len(got) != 1 || got[0] != es.LaneBackground {
		t.Fatalf("lanes = %v, want the background lane", got)
	}
}

func TestTheCapacityCheckIsQuietBelowEightyPercentAndFailsClosed(t *testing.T) {
	h := newHarness(t)
	h.provisionVerified(t, wsID)
	h.routes.ipFilters = 79
	if err := h.m.Capacity.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(h.ops.alerts) != 0 {
		t.Fatalf("alerts = %v", h.ops.alerts)
	}
	h.routes.filtersErr = errors.New("throttled")
	if err := h.m.Capacity.Execute(context.Background()); err == nil {
		t.Fatal("an unreadable count must be reported, never treated as zero")
	}
}

func TestTheHealthSweepFillsTheAdminListWithoutCallingTheProviderPerRow(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.provisionVerified(t, wsID)
	h.provider.health = es.TenantHealth{Sending: es.TenantEnabled, Findings: []es.FindingEvent{{Impact: es.FindingHigh, Type: "DKIM", Open: true}}}
	before, _, err := h.m.Admin.List(ctx, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if before[0].Health != nil || before[0].HealthCheckedAt != nil {
		t.Fatalf("no sweep yet, the row shows no health: %+v", before[0])
	}
	if err := h.m.HealthSweep.Execute(ctx); err != nil {
		t.Fatal(err)
	}
	calls := h.provider.healthCalls
	views, _, err := h.m.Admin.List(ctx, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if h.provider.healthCalls != calls {
		t.Fatalf("the admin list called the provider %d times", h.provider.healthCalls-calls)
	}
	if views[0].Health == nil || len(views[0].Health.Findings) != 1 || views[0].HealthCheckedAt == nil || !views[0].HealthCheckedAt.Equal(h.now) {
		t.Fatalf("view = %+v", views[0])
	}
	if got := h.provider.lanesOf("tenant health"); len(got) != 1 || got[0] != es.LaneBackground {
		t.Fatalf("lanes = %v, want the background lane", got)
	}
}

func TestAHealthCheckThatFailsIsShownWithItsError(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.provisionVerified(t, wsID)
	h.provider.healthErr = errors.New("ses down")
	if err := h.m.HealthSweep.Execute(ctx); err == nil {
		t.Fatal("the sweep reports the failure")
	}
	views, _, _ := h.m.Admin.List(ctx, 0, 10)
	if views[0].Health != nil || !strings.Contains(views[0].HealthError, "ses down") || views[0].HealthCheckedAt == nil {
		t.Fatalf("view = %+v", views[0])
	}
}

func TestProviderCallsRunInTheLaneOfTheirPurpose(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.provisionVerified(t, wsID)
	if err := h.m.Replies.Pause(ctx, wsID); err != nil {
		t.Fatal(err)
	}
	h.pauses.balances[wsID] = 100
	if err := h.m.ReplySweep.Execute(ctx); err != nil {
		t.Fatal(err)
	}
	for _, call := range []string{"pause", "resume", "unblock"} {
		if got := h.routes.lanesOf(call); len(got) == 0 || slices.ContainsFunc(got, func(l es.ProviderLane) bool { return l != es.LaneProtect }) {
			t.Errorf("%s lanes = %v, want protect", call, got)
		}
	}
	if got := h.routes.lanesOf("adopt"); len(got) != 1 || got[0] != es.LaneAccount {
		t.Errorf("adopt lanes = %v, want account", got)
	}
	pending := &es.SendingDomain{WorkspaceID: wsID, Domain: "loja.com.br", Kind: es.DomainOwn, Status: es.DomainVerifying, ConnectMethod: es.ConnectManual}
	if err := h.domains.Create(ctx, pending); err != nil {
		t.Fatal(err)
	}
	before := len(h.provider.lanesOf("identity status"))
	if err := h.m.Verification.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	if got := h.provider.lanesOf("identity status")[before:]; len(got) == 0 || got[0] != es.LaneBackground {
		t.Errorf("verification lanes = %v, want background", got)
	}
}

func TestTheGatewayAsksThePacerBeforeEverySendInItsRunKind(t *testing.T) {
	h := newHarness(t)
	h.provisionVerified(t, wsID)
	if _, err := h.m.Gateway.Send(context.Background(), marketingRequest(), compose("Oi")); err != nil {
		t.Fatal(err)
	}
	want := es.SendTicket{WorkspaceID: wsID, RunKind: marketingRequest().RunKind}
	if len(h.pacer.tickets) != 1 || h.pacer.tickets[0] != want {
		t.Fatalf("tickets = %+v, want %+v", h.pacer.tickets, want)
	}
}

func TestAThrottledSendGivesBackItsDailySlotAndRecordsNothing(t *testing.T) {
	h := newHarness(t)
	h.provisionVerified(t, wsID)
	h.pacer.err = &es.SendError{Kind: es.SendErrorTransient, Detail: "ses pacing", RetryAfter: time.Second}
	_, err := h.m.Gateway.Send(context.Background(), marketingRequest(), compose("Oi"))
	if retry, ok := es.SendRetryAfter(err); !ok || retry != time.Second {
		t.Fatalf("err = %v, want the transient throttle with its hint", err)
	}
	if len(h.provider.sent) != 0 || len(h.messages.rows) != 0 {
		t.Fatalf("sent %d rows %d, want nothing", len(h.provider.sent), len(h.messages.rows))
	}
	if h.usage.givebacks != 1 {
		t.Fatalf("givebacks = %d, want the daily slot returned", h.usage.givebacks)
	}
}
