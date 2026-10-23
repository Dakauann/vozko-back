package emailinbox

import (
	"context"
	"errors"
	"strings"
	"testing"

	"vozko/domain/balance"
	"vozko/domain/conversation"
	em "vozko/domain/emailinbox"
	"vozko/domain/emailsending"
	"vozko/domain/media"
	"vozko/domain/shared"
)

type adapterWorld struct {
	convs        *fakeConversations
	contacts     *fakeContacts
	accounts     *fakeAccounts
	suppressions *fakeSuppressions
	billing      *fakeBilling
	gateway      *fakeGateway
	files        *fakeRemoteFiles
	adapter      *ChannelAdapter
	order        []string
	conv         *em.Conversation
}

func newAdapterWorld(t *testing.T) *adapterWorld {
	t.Helper()
	w := &adapterWorld{
		convs:        newFakeConversations(),
		contacts:     newFakeContacts(),
		accounts:     &fakeAccounts{byWorkspace: map[string]*emailsending.SenderAccount{wsA: {TenantName: "ws-a", Mode: emailsending.ModeProbation, Status: emailsending.AccountActive}}},
		suppressions: &fakeSuppressions{suppressed: map[string]bool{}},
	}
	w.billing = &fakeBilling{order: &w.order}
	w.gateway = &fakeGateway{order: &w.order}
	w.files = &fakeRemoteFiles{files: map[string]media.RemoteFile{}}
	contact, _ := w.contacts.FindOrCreate(context.Background(), em.FindOrCreateContactInput{WorkspaceID: wsA, Address: "ana@cliente.com", DisplayName: "Ana"})
	w.conv = w.convs.add(&em.Conversation{
		ID: "conv-1", WorkspaceID: wsA, ContactID: contact.ID, SenderID: senderA,
		Subject: "Oferta de outubro", LastInboundMessageID: "m2@cliente.com", References: []string{"out-1@loja", "m1@cliente.com", "m2@cliente.com"},
	})
	w.adapter = NewChannelAdapter(AdapterDeps{
		Conversations: w.convs, Contacts: w.contacts, Accounts: w.accounts, Suppressions: w.suppressions, Billing: w.billing, Gateway: w.gateway, Files: w.files, Media: &fakeMedia{},
	})
	return w
}

func (w *adapterWorld) entry(t *testing.T) *conversation.EntryContext {
	t.Helper()
	ec, err := w.adapter.ResolveEntry(context.Background(), w.conv.ID)
	if err != nil {
		t.Fatal(err)
	}
	return ec
}

func TestAdapterDescribesItself(t *testing.T) {
	w := newAdapterWorld(t)
	if w.adapter.EntryType() != shared.EntryTypeEmail {
		t.Fatal("entry type")
	}
	if len(conversation.MediaLimitsOf(w.adapter.Capabilities().MediaLimits)) != 4 {
		t.Error("the composer must offer images, videos, audio and documents")
	}
	ec := w.entry(t)
	if ec.AccountID != senderA || ec.ContactRef != "ana@cliente.com" || ec.WorkspaceID != wsA {
		t.Errorf("entry = %+v", ec)
	}
}

func TestSendTextChargesBeforeSendingAndThreads(t *testing.T) {
	w := newAdapterWorld(t)
	outcome, err := w.adapter.SendText(context.Background(), w.entry(t), conversation.SendTextRequest{Body: "Olá Ana,\n\nsegue o preço <R$ 10>."})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(w.order, ",") != "charge,send" {
		t.Fatalf("order = %v", w.order)
	}
	if len(w.billing.charges) != 1 || !strings.HasPrefix(w.billing.charges[0], wsA+"|email_reply:conv-1:") {
		t.Errorf("charges = %v", w.billing.charges)
	}
	req := w.gateway.requests[0]
	if req.Kind != emailsending.KindTransactional || req.RunKind != emailsending.RunReply || req.RunID != "conv-1" || req.SenderID != senderA ||
		req.To != "ana@cliente.com" || req.InReplyTo != "m2@cliente.com" || len(req.References) != 3 {
		t.Errorf("request = %+v", req)
	}
	content := w.gateway.contents[0]
	if content.Subject != "Re: Oferta de outubro" || content.Text != "Olá Ana,\n\nsegue o preço <R$ 10>." {
		t.Errorf("content = %+v", content)
	}
	if strings.Contains(content.HTML, "<R$") || !strings.Contains(content.HTML, "&lt;R$ 10&gt;") || strings.Contains(content.HTML, "<img") {
		t.Errorf("html must escape the text and carry no tracking: %s", content.HTML)
	}
	if outcome.ProviderMessageID != "ses-conv-1" || outcome.Metadata[em.MetadataSubject] != "Re: Oferta de outubro" {
		t.Errorf("outcome = %+v", outcome)
	}
	if len(w.convs.outbound) != 1 {
		t.Error("outbound clock not advanced")
	}
}

func TestSendTextRefusals(t *testing.T) {
	cases := []struct {
		name        string
		body        string
		short       bool
		sendErr     error
		wantCode    string
		wantSends   int
		wantRefunds int
		wantErr     error
	}{
		{"empty body", "   ", false, nil, "send_failed", 0, 0, em.ErrEmptyReply},
		{"too long", strings.Repeat("a", em.MaxReplyTextBytes+1), false, nil, "send_failed", 0, 0, em.ErrReplyTooLong},
		{"wallet short is a coded refusal and nothing is sent", "oi", true, nil, SendCodeBalanceInsufficient, 0, 0, balance.ErrInsufficientBalance},
		{"provider failure refunds the charge", "oi", false, emailsending.ErrAccountPaused, "send_failed", 1, 1, emailsending.ErrAccountPaused},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := newAdapterWorld(t)
			w.billing.short, w.gateway.err = tc.short, tc.sendErr
			_, err := w.adapter.SendText(context.Background(), w.entry(t), conversation.SendTextRequest{Body: tc.body})
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if got := conversation.SendFailureCode(err, "send_failed"); got != tc.wantCode {
				t.Errorf("code = %q, want %q", got, tc.wantCode)
			}
			if len(w.gateway.requests) != tc.wantSends || len(w.billing.refunds) != tc.wantRefunds {
				t.Errorf("sends=%d refunds=%d", len(w.gateway.requests), len(w.billing.refunds))
			}
			if tc.wantRefunds > 0 && w.billing.refunds[0] != w.billing.charges[0] {
				t.Errorf("refund %q does not match charge %q", w.billing.refunds[0], w.billing.charges[0])
			}
			if len(w.convs.outbound) != 0 {
				t.Error("a refused send must not advance the outbound clock")
			}
		})
	}
}

func TestWindowStates(t *testing.T) {
	cases := []struct {
		name       string
		account    *emailsending.SenderAccount
		suppressed bool
		open       bool
		detail     string
	}{
		{"active", &emailsending.SenderAccount{TenantName: "ws-a", Mode: emailsending.ModeTrusted, Status: emailsending.AccountActive}, false, true, ""},
		{"not provisioned", nil, false, false, em.ProviderDetailSenderNotProvisioned},
		{"paused", &emailsending.SenderAccount{TenantName: "ws-a", Mode: emailsending.ModeTrusted, Status: emailsending.AccountPausedByPlatform}, false, false, em.ProviderDetailSenderPaused},
		{"suppressed address", &emailsending.SenderAccount{TenantName: "ws-a", Mode: emailsending.ModeTrusted, Status: emailsending.AccountActive}, true, false, em.ProviderDetailAddressSuppressed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := newAdapterWorld(t)
			if tc.account == nil {
				delete(w.accounts.byWorkspace, wsA)
			} else {
				w.accounts.byWorkspace[wsA] = tc.account
			}
			w.suppressions.suppressed[wsA+"|ana@cliente.com"] = tc.suppressed

			single, err := w.adapter.WindowState(context.Background(), w.entry(t))
			if err != nil {
				t.Fatal(err)
			}
			batch, err := w.adapter.WindowStates(context.Background(), []string{w.conv.ID, "missing"})
			if err != nil {
				t.Fatal(err)
			}
			for name, state := range map[string]conversation.WindowState{"single": single, "batch": batch[w.conv.ID]} {
				if state.Open != tc.open || state.ProviderDetail != tc.detail {
					t.Errorf("%s = %+v, want open=%v detail=%q", name, state, tc.open, tc.detail)
				}
			}
			if _, listed := batch["missing"]; listed {
				t.Error("unknown ids are left to the caller's unavailable fallback")
			}
		})
	}
}
