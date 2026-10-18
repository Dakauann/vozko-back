package balance_usecase

import (
	"errors"
	"strings"
	"testing"

	"vozko/domain/balance"
	"vozko/domain/cache"
	"vozko/domain/notification"
	"vozko/domain/workspace"
	"vozko/usecases/notification/notificationtest"
)

type fixedLowBalances []balance.LowBalanceRow

func (f fixedLowBalances) ListWorkspacesBelowBalance(int64) ([]balance.LowBalanceRow, error) {
	return f, nil
}

type memorySet struct {
	cache.SharedState
	members map[string]bool
}

func newMemorySet(members ...string) *memorySet {
	s := &memorySet{members: map[string]bool{}}
	for _, m := range members {
		s.members[m] = true
	}
	return s
}

func (s *memorySet) SMembers(string) ([]string, error) {
	out := make([]string, 0, len(s.members))
	for m := range s.members {
		out = append(out, m)
	}
	return out, nil
}

func (s *memorySet) SAdd(_ string, members ...string) error {
	for _, m := range members {
		s.members[m] = true
	}
	return nil
}

func (s *memorySet) SRem(_ string, members ...string) error {
	for _, m := range members {
		delete(s.members, m)
	}
	return nil
}

func TestLowBalanceNotifiesTheBillingTeamOnceUntilTheBalanceRecovers(t *testing.T) {
	publisher := &notificationtest.Publisher{}
	state := newMemorySet("recovered-ws")
	monitor := NewMonitorLowBalanceUseCase(fixedLowBalances{{WorkspaceID: "ws-1", AmountMicros: 2_300_000, Currency: "BRL"}}, publisher, state, "https://app.vozko.test/", 0)

	if n, err := monitor.Run(); err != nil || n != 1 {
		t.Fatalf("run = %d, %v", n, err)
	}
	d := publisher.Only(t)
	if d.WorkspaceID != "ws-1" || d.Category != notification.CategoryBilling || d.Severity != notification.SeverityWarning || !d.Email {
		t.Fatalf("draft = %+v", d)
	}
	if !d.Audience.Equal(notification.Holding(workspace.ResourceBalance, workspace.ActionRead)) {
		t.Fatalf("audience = %+v, want the people who see the balance", d.Audience)
	}
	if !strings.Contains(d.Body, "R$ 2,30") || d.ActionURL != "/dashboard/balance" {
		t.Fatalf("body %q link %q", d.Body, d.ActionURL)
	}
	if d.EmailCopy == nil || d.EmailCopy.Template != "low_balance_warning.html" || d.EmailCopy.Placeholders["Balance"] != "R$ 2,30" ||
		d.EmailCopy.Placeholders["DashboardURL"] != "https://app.vozko.test/dashboard/balance" {
		t.Fatalf("email = %+v", d.EmailCopy)
	}
	if !state.members["ws-1"] || state.members["recovered-ws"] {
		t.Fatalf("alerted set = %v; ws-1 marked, recovered workspace re-armed", state.members)
	}

	if _, err := monitor.Run(); err != nil {
		t.Fatal(err)
	}
	if len(publisher.Drafts()) != 1 {
		t.Fatal("a workspace already alerted is not notified again while the balance stays low")
	}
}

func TestAFailedPublishIsRetriedOnTheNextRun(t *testing.T) {
	publisher := &notificationtest.Publisher{Err: errors.New("db down")}
	state := newMemorySet()
	monitor := NewMonitorLowBalanceUseCase(fixedLowBalances{{WorkspaceID: "ws-1", AmountMicros: 1, Currency: "USD"}}, publisher, state, "https://app.vozko.test", 0)

	if _, err := monitor.Run(); err != nil {
		t.Fatal(err)
	}
	if state.members["ws-1"] {
		t.Fatal("an alert that was not published must not be marked as sent")
	}
	publisher.Err = nil
	_, _ = monitor.Run()
	if len(publisher.Drafts()) != 2 || !state.members["ws-1"] {
		t.Fatalf("the next run retries and then marks it: drafts=%d", len(publisher.Drafts()))
	}
}
