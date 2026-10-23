package readiness_usecase

import (
	"context"
	"errors"
	"testing"

	es "vozko/domain/emailsending"
	"vozko/domain/readiness"
	"vozko/domain/workspace"
)

type emailAccounts struct {
	account *es.SenderAccount
	err     error
}

func (a emailAccounts) GetByWorkspace(context.Context, string) (*es.SenderAccount, error) {
	return a.account, a.err
}

type emailSenders []es.Sender

func (s emailSenders) ListByWorkspace(context.Context, string) ([]es.Sender, error) { return s, nil }

type emailDomains []es.SendingDomain

func (d emailDomains) ListByWorkspace(context.Context, string) ([]es.SendingDomain, error) {
	return d, nil
}

type configureOnly struct{ allow bool }

func (c configureOnly) Execute(_ string, _ string, resource workspace.Resource, action workspace.Action) error {
	if c.allow && resource == workspace.ResourceEmailMarketing && action == workspace.ActionConfigure {
		return nil
	}
	return workspace.ErrInsufficientPermissions
}

func TestEmailProbeCountsTheSendersThatCanSendNow(t *testing.T) {
	active := &es.SenderAccount{TenantName: "t-1", Mode: es.ModeProbation, Status: es.AccountActive}
	paused := &es.SenderAccount{TenantName: "t-1", Mode: es.ModeProbation, Status: es.AccountPausedByPlatform}
	dormant := &es.SenderAccount{TenantName: "t-1", Mode: es.ModeProbation, Status: es.AccountDormant}
	domains := emailDomains{
		{ID: "verified", Domain: "news.acme.com.br", Kind: es.DomainSharedProbation, Status: es.DomainVerified},
		{ID: "pending", Domain: "mail.acme.com.br", Kind: es.DomainOwn, Status: es.DomainPending},
	}
	senders := emailSenders{{ID: "a", DomainID: "verified"}, {ID: "b", DomainID: "verified"}, {ID: "c", DomainID: "pending"}, {ID: "d", DomainID: "gone"}}
	cases := []struct {
		name    string
		deps    EmailProbeDeps
		count   int
		blocker readiness.Blocker
		err     bool
	}{
		{"ready", EmailProbeDeps{Accounts: emailAccounts{account: active}, Senders: senders, Domains: domains, Access: configureOnly{allow: true}}, 2, "", false},
		{"no permission", EmailProbeDeps{Accounts: emailAccounts{account: active}, Senders: senders, Domains: domains, Access: configureOnly{}}, 2, readiness.BlockerNoPermission, false},
		{"never opened", EmailProbeDeps{Accounts: emailAccounts{err: es.ErrAccountNotFound}, Senders: senders, Domains: domains, Access: configureOnly{allow: true}}, 0, "", false},
		{"paused account", EmailProbeDeps{Accounts: emailAccounts{account: paused}, Senders: senders, Domains: domains, Access: configureOnly{allow: true}}, 0, "", false},
		{"dormant account wakes on the next send", EmailProbeDeps{Accounts: emailAccounts{account: dormant}, Senders: senders, Domains: domains, Access: configureOnly{allow: true}}, 2, "", false},
		{"unreadable", EmailProbeDeps{Accounts: emailAccounts{err: errors.New("db down")}, Senders: senders, Domains: domains, Access: configureOnly{allow: true}}, 0, "", true},
	}
	for _, tc := range cases {
		probe := NewEmailProbe(tc.deps)
		if probe.Capability() != readiness.Email {
			t.Fatalf("capability = %s", probe.Capability())
		}
		status, err := probe.Probe(context.Background(), owner)
		if (err != nil) != tc.err {
			t.Errorf("%s: err = %v", tc.name, err)
			continue
		}
		if tc.err {
			continue
		}
		if status.Count != tc.count || status.Blocker != tc.blocker || status.CanAdd != (tc.blocker == "") {
			t.Errorf("%s: status = %+v", tc.name, status)
		}
	}
}
