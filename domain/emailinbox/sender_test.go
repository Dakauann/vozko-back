package emailinbox

import (
	"testing"
	"time"

	"vozko/domain/emailsending"
)

func TestPickSender(t *testing.T) {
	at := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	other := emailsending.Sender{ID: "s-other", DomainID: "d-2", IsDefault: true, CreatedAt: at}
	older := emailsending.Sender{ID: "s-older", DomainID: "d-1", CreatedAt: at}
	newer := emailsending.Sender{ID: "s-newer", DomainID: "d-1", CreatedAt: at.Add(time.Hour)}
	onDomainDefault := emailsending.Sender{ID: "s-default", DomainID: "d-1", IsDefault: true, CreatedAt: at.Add(2 * time.Hour)}

	cases := []struct {
		name    string
		senders []emailsending.Sender
		domain  string
		want    string
		found   bool
	}{
		{"default on the domain wins", []emailsending.Sender{other, newer, onDomainDefault, older}, "d-1", "s-default", true},
		{"oldest on the domain next", []emailsending.Sender{other, newer, older}, "d-1", "s-older", true},
		{"workspace default when the domain has none", []emailsending.Sender{newer, other}, "d-9", "s-other", true},
		{"oldest anywhere last", []emailsending.Sender{newer, older}, "d-9", "s-older", true},
		{"none", nil, "d-1", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := PickSender(tc.senders, tc.domain)
			if ok != tc.found || got.ID != tc.want {
				t.Fatalf("PickSender = (%q, %v), want (%q, %v)", got.ID, ok, tc.want, tc.found)
			}
		})
	}
}

func TestInboxForSender(t *testing.T) {
	domain := emailsending.SendingDomain{ID: "d-1", WorkspaceID: "ws-1", Domain: "loja.vozkoenvios.com.br"}
	sender := emailsending.Sender{ID: "s-1", WorkspaceID: "ws-1", DomainID: "d-1", LocalPart: "vendas", DisplayName: "Loja"}

	got := InboxForSender(sender, domain)
	if got.ID != "s-1" || got.WorkspaceID != "ws-1" || got.Name != "Loja" || got.Address != "vendas@loja.vozkoenvios.com.br" {
		t.Fatalf("InboxForSender = %+v", got)
	}
}

func TestInboxAutomation(t *testing.T) {
	agent := "agent-1"
	inbox := Inbox{AgentID: &agent, EnableAgentResponses: true, EnableAnalysis: true}
	got := inbox.Automation()
	if !got.HasAgent() || !got.EnableAgentResponses || !got.RunsAnalysis() || got.RunsWorkflows() {
		t.Fatalf("Automation = %+v", got)
	}
}

func TestContactName(t *testing.T) {
	cases := []struct {
		contact Contact
		want    string
	}{
		{Contact{Address: "ana@x.com", DisplayName: "Ana Lima"}, "Ana Lima"},
		{Contact{Address: "ana@x.com", DisplayName: "  "}, "ana@x.com"},
	}
	for _, tc := range cases {
		if got := tc.contact.Name(); got != tc.want {
			t.Errorf("Name() = %q, want %q", got, tc.want)
		}
	}
}
