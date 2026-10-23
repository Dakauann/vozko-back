package emailinbox

import (
	"context"
	"errors"
	"testing"

	em "vozko/domain/emailinbox"
	"vozko/domain/emailsending"
	"vozko/usecases/shared/automationrefs"
)

func inboxesUnderTest(refsErr error) (*Inboxes, *fakeInboxes) {
	domain := &emailsending.SendingDomain{ID: "d-a", WorkspaceID: wsA, Domain: "loja-a.com.br"}
	senders := &fakeSenders{all: []emailsending.Sender{{ID: senderA, WorkspaceID: wsA, DomainID: "d-a", LocalPart: "vendas", DisplayName: "Loja A"}}}
	repo := newFakeInboxes()
	domains := &fakeDomains{byID: map[string]*emailsending.SendingDomain{"d-a": domain}}
	return NewInboxes(repo, senders, domains, fakeRefs{err: refsErr}), repo
}

func TestInboxesListEnsuresOneInboxPerSender(t *testing.T) {
	uc, repo := inboxesUnderTest(nil)
	list, err := uc.List(context.Background(), wsA)
	if err != nil || len(list) != 1 || list[0].ID != senderA || list[0].Address != "vendas@loja-a.com.br" {
		t.Fatalf("List = %+v, %v", list, err)
	}
	if len(repo.byID) != 1 {
		t.Fatal("listing must create the inbox row so it can be configured before the first reply")
	}
	if other, _ := uc.List(context.Background(), wsB); len(other) != 0 {
		t.Fatal("another workspace saw the inbox")
	}
}

func TestInboxesUpdate(t *testing.T) {
	agent, dept := "agent-1", "dept-1"
	cases := []struct {
		name      string
		workspace string
		refsErr   error
		wantErr   error
	}{
		{"valid references are stored", wsA, nil, nil},
		{"foreign references are refused", wsA, automationrefs.ErrNotInWorkspace, em.ErrInvalidReference},
		{"another workspace cannot reach the inbox", wsB, nil, em.ErrInboxNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			uc, repo := inboxesUnderTest(tc.refsErr)
			got, err := uc.Update(context.Background(), tc.workspace, senderA, em.InboxSettings{AgentID: &agent, DepartmentID: &dept, EnableAgentResponses: true})
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr != nil {
				if stored, ok := repo.byID[senderA]; ok && stored.AgentID != nil {
					t.Fatal("a refused update must not be stored")
				}
				return
			}
			if got.AgentID == nil || *got.AgentID != agent || got.DepartmentID == nil || !got.EnableAgentResponses {
				t.Fatalf("stored = %+v", got)
			}
		})
	}
}
