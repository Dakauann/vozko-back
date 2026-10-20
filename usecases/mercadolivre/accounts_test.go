package mercadolivre

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/agent"
	mldomain "vozko/domain/mercadolivre"
	"vozko/domain/pipeline"
	"vozko/domain/workflow"
	wd "vozko/domain/workspace/workspace_department"
	"vozko/usecases/shared/automationrefs"
)

type stubAgents map[string]*agent.Agent

func (s stubAgents) FindByID(id string) (*agent.Agent, error) {
	if a, ok := s[id]; ok {
		return a, nil
	}
	return nil, errors.New("missing")
}

type stubWorkflows map[string]*workflow.Workflow

func (s stubWorkflows) FindByID(id string) (*workflow.Workflow, error) {
	if w, ok := s[id]; ok {
		return w, nil
	}
	return nil, errors.New("missing")
}

type stubPipelines map[string]*pipeline.Pipeline

func (s stubPipelines) GetByID(_, id string) (*pipeline.Pipeline, error) {
	if p, ok := s[id]; ok {
		return p, nil
	}
	return nil, errors.New("missing")
}

type stubDepartments map[string]*wd.Department

func (s stubDepartments) GetDepartmentByID(id string) (*wd.Department, error) {
	if d, ok := s[id]; ok {
		return d, nil
	}
	return nil, errors.New("missing")
}

func refs() automationrefs.Finders {
	return automationrefs.Finders{
		Agents:      stubAgents{"agent-1": {WorkspaceID: "ws-1"}, "agent-x": {WorkspaceID: "ws-2"}},
		Workflows:   stubWorkflows{"wf-1": {WorkspaceID: "ws-1"}},
		Pipelines:   stubPipelines{"p-1": {WorkspaceID: "ws-1", ObjectType: pipeline.ObjectConversation}},
		Departments: stubDepartments{"d-1": {WorkspaceID: "ws-1"}},
	}
}

type accountsFixture struct {
	uc       *Accounts
	accounts *fakeAccounts
	oauth    *fakeOAuth
	alerts   *fakeAlerts
}

func newAccountsFixture() accountsFixture {
	accounts := newFakeAccounts()
	oauth := &fakeOAuth{}
	alerts := &fakeAlerts{}
	uc := NewAccounts(AccountsDeps{Accounts: accounts, OAuth: oauth, References: refs(), Alerts: alerts, Metrics: newFakeMetrics()})
	uc.now = fixedClock
	return accountsFixture{uc: uc, accounts: accounts, oauth: oauth, alerts: alerts}
}

func TestUpdateValidatesEveryReferenceAgainstTheWorkspace(t *testing.T) {
	cases := []struct {
		name string
		in   UpdateAccountInput
		err  error
	}{
		{"agent of the workspace", UpdateAccountInput{AgentID: strPtr("agent-1"), EnableAgentResponses: boolPtr(true)}, nil},
		{"agent of another workspace", UpdateAccountInput{AgentID: strPtr("agent-x")}, automationrefs.ErrNotInWorkspace},
		{"unknown workflow", UpdateAccountInput{WorkflowID: strPtr("wf-9")}, automationrefs.ErrNotInWorkspace},
		{"funnel and department", UpdateAccountInput{PipelineID: strPtr("p-1"), DepartmentID: strPtr("d-1")}, nil},
		{"clearing with a blank id", UpdateAccountInput{AgentID: strPtr("  ")}, nil},
		{"disclosure outside the charset", UpdateAccountInput{AutomationDisclosure: strPtr("Sou a \U0000201CIA\U0000201D")}, mldomain.ErrTextCharset},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newAccountsFixture()
			f.accounts.put(connectedAccount())
			in := tc.in
			in.WorkspaceID, in.ID = "ws-1", "acct-1"
			got, err := f.uc.Update(context.Background(), in)
			if !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
			if tc.err == nil && got == nil {
				t.Fatal("no account returned")
			}
			if tc.err != nil && f.accounts.get("acct-1").AgentID != nil {
				t.Fatal("a refused update must not be stored")
			}
		})
	}
}

func TestUpdateNeverCrossesWorkspaces(t *testing.T) {
	f := newAccountsFixture()
	f.accounts.put(connectedAccount())
	if _, err := f.uc.Update(context.Background(), UpdateAccountInput{WorkspaceID: "ws-2", ID: "acct-1"}); !errors.Is(err, mldomain.ErrAccountNotFound) {
		t.Fatalf("err = %v", err)
	}
	if _, err := f.uc.Get(context.Background(), "ws-2", "acct-1"); !errors.Is(err, mldomain.ErrAccountNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestDisconnectWipesFirstRevokesBestEffortAndSchedulesThePurge(t *testing.T) {
	f := newAccountsFixture()
	f.accounts.put(connectedAccount())
	f.oauth.revokeErr = errors.New("mercado livre down")
	out, err := f.uc.Disconnect(context.Background(), "ws-1", "acct-1")
	if err != nil {
		t.Fatal(err)
	}
	if !out.PurgeAfter.Equal(testNow.Add(30*24*time.Hour)) || out.Warning != "revoke_failed" {
		t.Fatalf("out = %+v", out)
	}
	stored := f.accounts.get("acct-1")
	if stored.AccessToken != "" || stored.RefreshToken != "" || stored.Status != mldomain.StatusDisconnected {
		t.Fatalf("stored = %+v", stored)
	}
	if len(f.oauth.revoked) != 1 || f.oauth.revoked[0].AccessToken != "APP_USR-1" {
		t.Fatalf("revoke = %+v", f.oauth.revoked)
	}
	if len(f.alerts.reconnected) != 1 {
		t.Fatal("a disconnect closes the reconnect alert")
	}
}

func TestDisconnectOfADeadAccountSkipsTheRevoke(t *testing.T) {
	f := newAccountsFixture()
	a := connectedAccount()
	a.Status, a.AccessToken, a.RefreshToken = mldomain.StatusNeedsReconnect, "", ""
	f.accounts.put(a)
	out, err := f.uc.Disconnect(context.Background(), "ws-1", "acct-1")
	if err != nil || out.Warning != "" || len(f.oauth.revoked) != 0 {
		t.Fatalf("out=%+v err=%v revoked=%v", out, err, f.oauth.revoked)
	}
}

func TestListNeedsAWorkspace(t *testing.T) {
	f := newAccountsFixture()
	if _, err := f.uc.List(context.Background(), mldomain.ListAccountsInput{}); !errors.Is(err, mldomain.ErrWorkspaceIDRequired) {
		t.Fatalf("err = %v", err)
	}
}
