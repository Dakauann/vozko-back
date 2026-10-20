package mercadolivre

import (
	"context"
	"errors"
	"testing"
	"time"

	mldomain "vozko/domain/mercadolivre"
)

type healthFixture struct {
	uc        *HealthCheck
	accounts  *fakeAccounts
	users     *fakeUsers
	questions *fakeQuestions
	messages  *fakeMessages
	alerts    *fakeAlerts
	contacts  *fakeContacts
	metrics   *fakeMetrics
}

func newHealthFixture() healthFixture {
	accounts := newFakeAccounts()
	alerts := &fakeAlerts{}
	metrics := newFakeMetrics()
	oauth := &fakeOAuth{refresh: rotated}
	tokens := NewTokenSource(accounts, oauth, fakeClassifier{}, alerts, metrics)
	tokens.now = fixedClock
	f := healthFixture{
		accounts:  accounts,
		users:     &fakeUsers{me: &mldomain.User{ID: "415458330", Nickname: "LOJA NOVA", SiteID: mldomain.SiteMLB}},
		questions: &fakeQuestions{},
		messages:  &fakeMessages{},
		alerts:    alerts,
		contacts:  newFakeContacts(),
		metrics:   metrics,
	}
	f.uc = NewHealthCheck(HealthCheckDeps{
		Accounts: accounts,
		Contacts: f.contacts,
		Services: Services{OAuth: oauth, Users: f.users, Questions: f.questions, Messages: f.messages, Errors: fakeClassifier{}},
		Tokens:   tokens,
		Alerts:   alerts,
		Metrics:  metrics,
	})
	f.uc.now = fixedClock
	return f
}

func TestHealthCheckRefreshesNicknameAndCapabilities(t *testing.T) {
	f := newHealthFixture()
	f.accounts.put(connectedAccount())
	f.questions.searchErr = &mlErr{msg: "PA_UNAUTHORIZED_RESULT_FROM_POLICIES", perm: true}

	got, err := f.uc.Check(context.Background(), f.accounts.get("acct-1"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Nickname != "LOJA NOVA" || got.CanReadQuestions || got.CanAnswerQuestions || !got.CanSendMessages {
		t.Fatalf("account = %+v", got)
	}
	if got.LastHealthCheckAt == nil || !got.LastHealthCheckAt.Equal(testNow) {
		t.Fatalf("checked at = %v", got.LastHealthCheckAt)
	}
}

func TestAnInconclusiveProbeKeepsTheCapabilities(t *testing.T) {
	f := newHealthFixture()
	f.accounts.put(connectedAccount())
	f.messages.unreadErr = &mlErr{msg: "timeout", retryable: true}
	if _, err := f.uc.Check(context.Background(), f.accounts.get("acct-1")); !errors.Is(err, ErrProbeInconclusive) {
		t.Fatalf("err = %v", err)
	}
	if got := f.accounts.get("acct-1"); !got.CanSendMessages || !got.CanReadQuestions {
		t.Fatalf("an unreachable probe must not switch capabilities off: %+v", got)
	}
}

func TestADeadGrantFoundByTheHealthCheckNeedsReconnect(t *testing.T) {
	f := newHealthFixture()
	f.accounts.put(connectedAccount())
	f.users.meErr = &mlErr{msg: "invalid_token", auth: true}
	f.uc.tokens.oauth.(*fakeOAuth).refresh = func(mldomain.RefreshCredentials) (*mldomain.TokenGrant, error) {
		return nil, &mlErr{msg: "invalid_grant", auth: true}
	}
	if _, err := f.uc.Check(context.Background(), f.accounts.get("acct-1")); err != nil {
		t.Fatal(err)
	}
	if got := f.accounts.get("acct-1"); got.Status != mldomain.StatusNeedsReconnect || len(f.alerts.alerted) != 1 {
		t.Fatalf("account = %+v alerts = %v", got, f.alerts.alerted)
	}
}

func TestTheDailyJobRemindsAccountsStillWaitingForAReconnect(t *testing.T) {
	f := newHealthFixture()
	dead := connectedAccount()
	dead.Status, dead.AccessToken, dead.RefreshToken = mldomain.StatusNeedsReconnect, "", ""
	f.accounts.put(dead)
	healthy := connectedAccount()
	healthy.ID, healthy.MLUserID = "acct-2", "2"
	yesterday := testNow.Add(-25 * time.Hour)
	healthy.LastHealthCheckAt = &yesterday
	f.accounts.put(healthy)

	if err := f.uc.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(f.alerts.alerted) != 1 || f.alerts.alerted[0] != "acct-1" {
		t.Fatalf("alerts = %v", f.alerts.alerted)
	}
	for _, id := range []string{"acct-1", "acct-2"} {
		if got := f.accounts.get(id); got.LastHealthCheckAt == nil || !got.LastHealthCheckAt.Equal(testNow) {
			t.Fatalf("%s not marked checked", id)
		}
	}
	if err := f.uc.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(f.alerts.alerted) != 1 {
		t.Fatal("an account checked today is not checked again")
	}
}

func TestAManualCheckStaysInsideTheWorkspace(t *testing.T) {
	f := newHealthFixture()
	f.accounts.put(connectedAccount())
	if _, err := f.uc.CheckInWorkspace(context.Background(), "ws-2", "acct-1"); !errors.Is(err, mldomain.ErrAccountNotFound) {
		t.Fatalf("err = %v", err)
	}
	got, err := f.uc.CheckInWorkspace(context.Background(), "ws-1", "acct-1")
	if err != nil || got.Nickname != "LOJA NOVA" {
		t.Fatalf("got = %+v err = %v", got, err)
	}
}

func (f healthFixture) asker(id, accountID, mlUserID string, blockedAt *time.Time) {
	f.contacts.rows[id] = &mldomain.Contact{ID: id, WorkspaceID: "ws-1", AccountID: accountID, MLUserID: mlUserID, QuestionsBlockedAt: blockedAt}
}

func (f healthFixture) blocked(id string) *time.Time {
	return f.contacts.rows[id].QuestionsBlockedAt
}

func dueAccount() *mldomain.Account {
	a := connectedAccount()
	yesterday := testNow.Add(-25 * time.Hour)
	a.LastHealthCheckAt = &yesterday
	return a
}

func TestTheDailyHealthCheckMirrorsTheQuestionBlacklist(t *testing.T) {
	f := newHealthFixture()
	f.accounts.put(dueAccount())
	earlier := testNow.Add(-48 * time.Hour)
	f.asker("ct-vozko", "acct-1", "111", &earlier)
	f.asker("ct-outside", "acct-1", "222", nil)
	f.asker("ct-unblocked", "acct-1", "333", &earlier)
	f.asker("ct-other-account", "acct-9", "444", &earlier)
	f.questions.blacklist = []mldomain.BlacklistPage{
		{UserIDs: []string{"111", "222"}, Total: 3, Limit: 2},
		{UserIDs: []string{"555"}, Total: 3, Offset: 2, Limit: 2},
	}

	if err := f.uc.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := f.blocked("ct-vozko"); got == nil || !got.Equal(earlier) {
		t.Fatalf("a block already mirrored keeps its time: %v", got)
	}
	if got := f.blocked("ct-outside"); got == nil || !got.Equal(testNow) {
		t.Fatalf("a block made outside the platform is mirrored: %v", got)
	}
	if f.blocked("ct-unblocked") != nil {
		t.Fatal("a block lifted outside the platform is cleared")
	}
	if f.blocked("ct-other-account") == nil {
		t.Fatal("another account's mirror is untouched")
	}
	if len(f.questions.blacklistAsks) != 2 || f.questions.blacklistAsks[0] != 0 || f.questions.blacklistAsks[1] != 2 {
		t.Fatalf("offsets = %v", f.questions.blacklistAsks)
	}
	for _, background := range f.questions.priorities {
		if !background {
			t.Fatal("the blacklist is read at background priority")
		}
	}
	if f.metrics.count("questions/blacklist_synced") != 1 {
		t.Fatalf("metrics = %v", f.metrics.events)
	}
}

func TestAnIncompleteBlacklistOnlyAddsBlocks(t *testing.T) {
	f := newHealthFixture()
	f.accounts.put(dueAccount())
	earlier := testNow.Add(-48 * time.Hour)
	f.asker("ct-outside", "acct-1", "222", nil)
	f.asker("ct-unlisted", "acct-1", "333", &earlier)
	f.questions.blacklist = []mldomain.BlacklistPage{
		{UserIDs: []string{"222"}, Total: 2},
		{Total: 2, Offset: 1},
	}
	if err := f.uc.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	if f.blocked("ct-outside") == nil || f.blocked("ct-unlisted") == nil || f.contacts.clears != 0 {
		t.Fatalf("outside=%v unlisted=%v clears=%d", f.blocked("ct-outside"), f.blocked("ct-unlisted"), f.contacts.clears)
	}
	if f.metrics.count("questions/blacklist_partial") != 1 {
		t.Fatalf("metrics = %v", f.metrics.events)
	}
}

func TestAnUnreadableBlacklistLeavesTheMirrorAndTheCheckHealthy(t *testing.T) {
	f := newHealthFixture()
	f.accounts.put(dueAccount())
	earlier := testNow.Add(-48 * time.Hour)
	f.asker("ct-vozko", "acct-1", "111", &earlier)
	f.questions.blacklistErr = &mlErr{msg: "timeout", retryable: true}
	if err := f.uc.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	if f.blocked("ct-vozko") == nil || f.contacts.marks+f.contacts.clears != 0 {
		t.Fatal("an unreadable blacklist must not touch the mirror")
	}
	if f.metrics.count("questions/blacklist_failed") != 1 || f.metrics.count("health/healthy") != 1 {
		t.Fatalf("metrics = %v", f.metrics.events)
	}
}

func TestTheBlacklistIsReadOnlyByTheDailyCheckOfAnAccountThatReadsQuestions(t *testing.T) {
	f := newHealthFixture()
	f.accounts.put(connectedAccount())
	if _, err := f.uc.CheckInWorkspace(context.Background(), "ws-1", "acct-1"); err != nil {
		t.Fatal(err)
	}
	if len(f.questions.blacklistAsks) != 0 {
		t.Fatal("a manual check must not page the blacklist")
	}

	g := newHealthFixture()
	g.accounts.put(dueAccount())
	g.questions.searchErr = &mlErr{msg: "PA_UNAUTHORIZED_RESULT_FROM_POLICIES", perm: true}
	if err := g.uc.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(g.questions.blacklistAsks) != 0 {
		t.Fatal("an account without the questions capability must not read the blacklist")
	}
}
