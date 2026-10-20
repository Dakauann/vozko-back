package tiktok

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"vozko/domain/agent"
	ca "vozko/domain/audience"
	"vozko/domain/pipeline"
	"vozko/domain/shared"
	ttdomain "vozko/domain/tiktok"
	"vozko/domain/workflow"
	wd "vozko/domain/workspace/workspace_department"
	"vozko/usecases/shared/automationrefs"
)

type refFinder struct{ workspace map[string]string }

func (f refFinder) FindByID(id string) (*agent.Agent, error) {
	if ws, ok := f.workspace[id]; ok {
		return &agent.Agent{ID: id, WorkspaceID: ws}, nil
	}
	return nil, errors.New("not found")
}

type workflowFinder struct{ workspace map[string]string }

func (f workflowFinder) FindByID(id string) (*workflow.Workflow, error) {
	if ws, ok := f.workspace[id]; ok {
		return &workflow.Workflow{ID: id, WorkspaceID: ws}, nil
	}
	return nil, errors.New("not found")
}

type pipelineFinder struct{}

func (pipelineFinder) GetByID(workspaceID, id string) (*pipeline.Pipeline, error) {
	return &pipeline.Pipeline{ID: id, WorkspaceID: workspaceID, ObjectType: pipeline.ObjectConversation}, nil
}

type departmentFinder struct{}

func (departmentFinder) GetDepartmentByID(id string) (*wd.Department, error) {
	return &wd.Department{ID: id, WorkspaceID: "ws-1"}, nil
}

func newAccountsForTest(accounts *fakeAccounts, svc *fakeServices) *Accounts {
	uc := NewAccounts(AccountsDeps{
		Accounts: accounts, OAuth: svc, Alerts: &fakeAlerts{}, Metrics: &fakeMetrics{},
		References: automationrefs.Finders{
			Agents:      refFinder{workspace: map[string]string{"agent-1": "ws-1", "agent-x": "ws-other"}},
			Workflows:   workflowFinder{workspace: map[string]string{"wf-1": "ws-1"}},
			Pipelines:   pipelineFinder{},
			Departments: departmentFinder{},
		},
	})
	uc.now = fixedClock
	return uc
}

func TestUpdateAppliesTheAutomationSettings(t *testing.T) {
	accounts := newFakeAccounts()
	a := accounts.put(connectedAccount())
	uc := newAccountsForTest(accounts, newFakeServices())
	yes := true
	updated, err := uc.Update(context.Background(), UpdateAccountInput{WorkspaceID: "ws-1", ID: a.ID, AgentID: ptrStr("agent-1"), WorkflowID: ptrStr("wf-1"), EnableAgentResponses: &yes, EnableAnalysis: &yes, AutomationDisclosure: ptrStr("  Sou uma assistente virtual.  ")})
	if err != nil {
		t.Fatal(err)
	}
	if *updated.AgentID != "agent-1" || *updated.WorkflowID != "wf-1" || !updated.EnableAgentResponses || !updated.EnableAnalysis || updated.AutomationDisclosure != "Sou uma assistente virtual." {
		t.Fatalf("updated = %+v", updated)
	}
}

func TestUpdateRefusesAnAgentOfAnotherWorkspace(t *testing.T) {
	accounts := newFakeAccounts()
	a := accounts.put(connectedAccount())
	uc := newAccountsForTest(accounts, newFakeServices())
	if _, err := uc.Update(context.Background(), UpdateAccountInput{WorkspaceID: "ws-1", ID: a.ID, AgentID: ptrStr("agent-x")}); !errors.Is(err, automationrefs.ErrNotInWorkspace) {
		t.Fatalf("err = %v", err)
	}
	if _, err := uc.Update(context.Background(), UpdateAccountInput{WorkspaceID: "ws-1", ID: a.ID, AgentID: ptrStr("ghost")}); !errors.Is(err, automationrefs.ErrNotInWorkspace) {
		t.Fatalf("unknown agent: %v", err)
	}
	if _, err := uc.Update(context.Background(), UpdateAccountInput{WorkspaceID: "ws-other", ID: a.ID}); !errors.Is(err, ttdomain.ErrAccountNotFound) {
		t.Fatalf("other workspace: %v", err)
	}
}

func TestUpdateRefusesAnOversizedDisclosure(t *testing.T) {
	accounts := newFakeAccounts()
	a := accounts.put(connectedAccount())
	uc := newAccountsForTest(accounts, newFakeServices())
	long := make([]rune, MaxDisclosureRunes+1)
	for i := range long {
		long[i] = 'x'
	}
	text := string(long)
	if _, err := uc.Update(context.Background(), UpdateAccountInput{WorkspaceID: "ws-1", ID: a.ID, AutomationDisclosure: &text}); !errors.Is(err, ErrDisclosureTooLong) {
		t.Fatalf("err = %v", err)
	}
}

func TestDisconnectWipesTokensRevokesAndSchedulesThePurge(t *testing.T) {
	accounts := newFakeAccounts()
	svc := newFakeServices()
	a := accounts.put(connectedAccount())
	uc := newAccountsForTest(accounts, svc)
	out, err := uc.Disconnect(context.Background(), "ws-1", a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !out.PurgeAfter.Equal(testNow.Add(ttdomain.PurgeGracePeriod)) || out.Warning != "" {
		t.Fatalf("out = %+v", out)
	}
	stored := accounts.get(a.ID)
	if stored.Status != ttdomain.StatusDisconnected || stored.AccessToken != "" || stored.PurgeAfter == nil {
		t.Fatalf("stored = %+v", stored)
	}
	if len(svc.revoked) != 1 || svc.revoked[0] != "act.1" {
		t.Fatalf("revoked = %v", svc.revoked)
	}
}

func newHealthForTest(accounts *fakeAccounts, svc *fakeServices) (*HealthCheckJob, *fakeAlerts) {
	alerts := &fakeAlerts{}
	metrics := &fakeMetrics{}
	tokens := NewTokenSource(accounts, svc, fakeClassifier{}, alerts, metrics)
	tokens.now = fixedClock
	job := NewHealthCheckJob(HealthCheckDeps{
		Accounts: accounts, Services: svc.services(fakeClassifier{}), Tokens: tokens,
		Webhooks: NewWebhookSubscriptions(svc, "https://api.example.com/webhooks/tiktok", metrics), Alerts: alerts, Metrics: metrics,
	})
	job.now = fixedClock
	return job, alerts
}

func TestHealthCheckRefreshesScopesAndProfile(t *testing.T) {
	accounts := newFakeAccounts()
	svc := newFakeServices()
	a := accounts.put(connectedAccount())
	svc.info = ttdomain.TokenInfo{OpenID: a.OpenID, Scopes: []string{ttdomain.ScopeVideoList, ttdomain.ScopeCommentList}}
	svc.profile = ttdomain.BusinessProfile{Username: "loja.nova", DisplayName: "Loja Nova", HasAdvancedAccess: true}
	job, alerts := newHealthForTest(accounts, svc)
	if err := job.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	stored := accounts.get(a.ID)
	if stored.Username != "loja.nova" || stored.Capabilities.CanSendMessages || !stored.Capabilities.CanReadComments || stored.LastHealthCheckAt == nil {
		t.Fatalf("stored = %+v", stored)
	}
	if !stored.WebhooksHealthy || len(alerts.healthy) != 1 {
		t.Errorf("webhooks %v alerts %+v", stored.WebhooksHealthy, alerts)
	}
}

func TestHealthCheckRevokesAnAccountWhoseTokenTikTokNoLongerAccepts(t *testing.T) {
	accounts := newFakeAccounts()
	svc := newFakeServices()
	a := accounts.put(connectedAccount())
	svc.infoErr = codedError{auth: true, refusal: true}
	job, alerts := newHealthForTest(accounts, svc)
	if err := job.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	stored := accounts.get(a.ID)
	if stored.Status != ttdomain.StatusRevoked || stored.AccessToken != "" || stored.PurgeAfter == nil {
		t.Fatalf("stored = %+v", stored)
	}
	if len(alerts.reconnect) != 1 {
		t.Errorf("alerts = %+v", alerts)
	}
}

func TestHealthCheckMarksLostPermissionsForReconnect(t *testing.T) {
	accounts := newFakeAccounts()
	svc := newFakeServices()
	a := accounts.put(connectedAccount())
	svc.info = ttdomain.TokenInfo{OpenID: a.OpenID, Scopes: []string{ttdomain.ScopeUserInfoBasic}}
	job, _ := newHealthForTest(accounts, svc)
	_ = job.Execute(context.Background())
	if stored := accounts.get(a.ID); stored.Status != ttdomain.StatusNeedsReconnect {
		t.Fatalf("stored = %+v", stored)
	}
}

func TestHealthCheckWarnsBeforeTheYearlyReconnect(t *testing.T) {
	accounts := newFakeAccounts()
	svc := newFakeServices()
	a := connectedAccount()
	soon := testNow.Add(10 * 24 * time.Hour)
	a.RefreshExpiresAt = &soon
	accounts.put(a)
	svc.info = ttdomain.TokenInfo{OpenID: a.OpenID, Scopes: []string{ttdomain.ScopeVideoList, ttdomain.ScopeCommentList}}
	job, alerts := newHealthForTest(accounts, svc)
	_ = job.Execute(context.Background())
	if len(alerts.soon) != 1 {
		t.Fatalf("alerts = %+v", alerts)
	}
}

type fakePurge struct {
	mu            sync.Mutex
	conversations []string
	contacts      []string
	keys          []string
	rowsLeft      int
	deleted       []string
	forgotten     []string
	contactsGone  []string
}

func (f *fakePurge) ConversationIDsForPurge(ctx context.Context, accountID string, limit int) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := min(limit, len(f.conversations))
	return append([]string(nil), f.conversations[:n]...), nil
}

func (f *fakePurge) DeleteConversations(ctx context.Context, accountID string, ids []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, ids...)
	f.conversations = f.conversations[len(ids):]
	return nil
}

func (f *fakePurge) StoredObjectKeys(ctx context.Context, accountID string, limit int) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.keys...), nil
}

func (f *fakePurge) ForgetObjectKeys(ctx context.Context, accountID string, keys []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.forgotten = append(f.forgotten, keys...)
	f.keys = nil
	return nil
}

func (f *fakePurge) ContactIDsForPurge(ctx context.Context, accountID string, limit int) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := min(limit, len(f.contacts))
	return append([]string(nil), f.contacts[:n]...), nil
}

func (f *fakePurge) DeleteContacts(ctx context.Context, accountID string, ids []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.contactsGone = append(f.contactsGone, ids...)
	f.contacts = f.contacts[len(ids):]
	return nil
}

func (f *fakePurge) DeleteAccountRows(ctx context.Context, accountID string, limit int) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.rowsLeft > 0 {
		f.rowsLeft--
		return false, nil
	}
	return true, nil
}

type fakeEntries struct {
	mu       sync.Mutex
	entries  [][]string
	contacts [][]string
	files    []string
	err      error
}

func (f *fakeEntries) PurgeEntries(ctx context.Context, entryType shared.EntryType, ids []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.entries = append(f.entries, append([]string(nil), ids...))
	return nil
}

func (f *fakeEntries) PurgeContacts(ctx context.Context, contactType shared.EntryType, ids []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.contacts = append(f.contacts, append([]string(nil), ids...))
	return nil
}

func (f *fakeEntries) DeleteFiles(ctx context.Context, keys []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.files = append(f.files, keys...)
	return nil
}

type fakeSourcePurge struct {
	mu    sync.Mutex
	calls []string
}

func (f *fakeSourcePurge) PurgeAccount(ctx context.Context, source shared.EntryType, accountID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, string(source)+":"+accountID)
	return nil
}

type fakeAudiencePurge struct {
	mu    sync.Mutex
	calls []string
}

func (f *fakeAudiencePurge) PurgeAccount(ctx context.Context, source ca.Source, accountID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, string(source)+":"+accountID)
	return nil
}

func dueAccount(accounts *fakeAccounts, status ttdomain.Status, purgeAfter time.Time) *ttdomain.Account {
	a := connectedAccount()
	a.Status = status
	a.PurgeAfter = &purgeAfter
	return accounts.put(a)
}

func newPurgeForTest(accounts *fakeAccounts, purge *fakePurge, entries *fakeEntries) (*PurgeJob, *fakeSourcePurge, *fakeAudiencePurge) {
	rulesPurge := &fakeSourcePurge{}
	audiencePurge := &fakeAudiencePurge{}
	job := NewPurgeJob(PurgeDeps{Accounts: accounts, Purge: purge, Entries: entries, Rules: rulesPurge, Audience: audiencePurge, Metrics: &fakeMetrics{}})
	job.now = fixedClock
	return job, rulesPurge, audiencePurge
}

func TestPurgeErasesEverythingOfADueAccountAndThenTheAccount(t *testing.T) {
	accounts := newFakeAccounts()
	a := dueAccount(accounts, ttdomain.StatusDisconnected, testNow.Add(-time.Minute))
	purge := &fakePurge{conversations: []string{"c1", "c2", "c3"}, contacts: []string{"p1", "p2"}, keys: []string{"tiktok/accounts/a/avatar"}, rowsLeft: 2}
	entries := &fakeEntries{}
	job, rulesPurge, audiencePurge := newPurgeForTest(accounts, purge, entries)
	if err := job.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(entries.entries) != 1 || len(entries.entries[0]) != 3 || len(purge.deleted) != 3 {
		t.Fatalf("erased %v deleted %v", entries.entries, purge.deleted)
	}
	if len(rulesPurge.calls) != 1 || len(audiencePurge.calls) != 1 || audiencePurge.calls[0] != "tiktok:"+a.ID {
		t.Fatalf("rules %v audience %v", rulesPurge.calls, audiencePurge.calls)
	}
	if len(entries.files) != 1 || len(purge.forgotten) != 1 {
		t.Fatalf("files %v forgotten %v", entries.files, purge.forgotten)
	}
	if len(entries.contacts) != 1 || len(purge.contactsGone) != 2 {
		t.Fatalf("contacts %v gone %v", entries.contacts, purge.contactsGone)
	}
	if accounts.get(a.ID) != nil {
		t.Fatal("the account row must be deleted last")
	}
}

func TestPurgeStopsAndKeepsTheAccountWhenErasureFails(t *testing.T) {
	accounts := newFakeAccounts()
	a := dueAccount(accounts, ttdomain.StatusRevoked, testNow.Add(-time.Minute))
	job, _, _ := newPurgeForTest(accounts, &fakePurge{conversations: []string{"c1"}}, &fakeEntries{err: errors.New("bucket down")})
	_ = job.Execute(context.Background())
	if accounts.get(a.ID) == nil {
		t.Fatal("a failed erasure must keep the account so the purge resumes")
	}
}

func TestPurgeLeavesAccountsThatAreNotDue(t *testing.T) {
	accounts := newFakeAccounts()
	a := dueAccount(accounts, ttdomain.StatusDisconnected, testNow.Add(time.Hour))
	entries := &fakeEntries{}
	job, _, _ := newPurgeForTest(accounts, &fakePurge{conversations: []string{"c1"}}, entries)
	_ = job.Execute(context.Background())
	if len(entries.entries) != 0 || accounts.get(a.ID) == nil {
		t.Fatal("an account inside its grace period must be kept")
	}
}

func TestPurgeStopsWhenTheAccountIsReconnectedMidway(t *testing.T) {
	accounts := newFakeAccounts()
	a := dueAccount(accounts, ttdomain.StatusDisconnected, testNow.Add(-time.Minute))
	purge := &fakePurge{conversations: []string{"c1"}}
	entries := &fakeEntries{}
	job, rulesPurge, _ := newPurgeForTest(accounts, purge, entries)
	reconnect := func() {
		cur := accounts.get(a.ID)
		cur.Status = ttdomain.StatusConnected
		cur.PurgeAfter = nil
		accounts.put(cur)
	}
	entries.err = nil
	purge.conversations = []string{"c1"}
	reconnect()
	_ = job.Execute(context.Background())
	if len(rulesPurge.calls) != 0 || accounts.get(a.ID) == nil {
		t.Fatal("a reconnected account must keep its data")
	}
}
