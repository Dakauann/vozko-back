package instagram

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"vozko/domain/conversation"
	igdomain "vozko/domain/instagram"
	"vozko/domain/shared"
	"vozko/infra/meta"
)

type adapterFixture struct {
	adapter   conversation.ChannelAdapter
	accounts  *fakeAccountRepo
	contacts  *fakeContactRepo
	convs     *fakeConversationRepo
	messaging *fakeMessagingService
}

func newAdapterFixture(t *testing.T, accounts []*igdomain.Account, lastInbound *time.Time) *adapterFixture {
	t.Helper()

	byID := map[string]*igdomain.Account{}
	for _, a := range accounts {
		byID[a.ID] = a
	}

	accountRepo := &fakeAccountRepo{
		FindByIDFn: func(_ context.Context, id string) (*igdomain.Account, error) {
			if a, ok := byID[id]; ok {
				return a, nil
			}
			return nil, igdomain.ErrAccountNotFound
		},
	}

	convRepo := &fakeConversationRepo{
		FindByIDFn: func(_ context.Context, id string) (*igdomain.Conversation, error) {
			for _, a := range accounts {
				if id == "conv-"+a.ID {
					return &igdomain.Conversation{
						ID:                    id,
						WorkspaceID:           a.WorkspaceID,
						IGAccountID:           a.ID,
						ContactID:             "contact-" + a.ID,
						LastCustomerMessageAt: lastInbound,
					}, nil
				}
			}
			return nil, igdomain.ErrConversationNotFound
		},
	}

	contactRepo := &fakeContactRepo{
		FindByIDFn: func(_ context.Context, id string) (*igdomain.Contact, error) {
			accountID := strings.TrimPrefix(id, "contact-")
			return &igdomain.Contact{
				ID:       id,
				IGSID:    "igsid-of-" + accountID,
				Username: "customer-" + accountID,
			}, nil
		},
	}

	messaging := &fakeMessagingService{}

	return &adapterFixture{
		adapter:   NewChannelAdapter(accountRepo, contactRepo, convRepo, messaging),
		accounts:  accountRepo,
		contacts:  contactRepo,
		convs:     convRepo,
		messaging: messaging,
	}
}

func openWindow() *time.Time {
	t := time.Now().UTC().Add(-time.Hour)
	return &t
}

func closedWindow() *time.Time {
	t := time.Now().UTC().Add(-25 * time.Hour)
	return &t
}

func TestChannelAdapter_EntryType(t *testing.T) {
	f := newAdapterFixture(t, []*igdomain.Account{connectedAccount()}, openWindow())
	if got := f.adapter.EntryType(); got != shared.EntryTypeInstagram {
		t.Fatalf("EntryType() = %q, want %q", got, shared.EntryTypeInstagram)
	}
}

func TestChannelAdapter_ResolveEntry(t *testing.T) {
	account := connectedAccount()
	f := newAdapterFixture(t, []*igdomain.Account{account}, openWindow())

	ec, err := f.adapter.ResolveEntry(context.Background(), "conv-"+account.ID)
	if err != nil {
		t.Fatalf("ResolveEntry: %v", err)
	}
	if ec.AccountID != account.ID {
		t.Errorf("AccountID = %q, want %q", ec.AccountID, account.ID)
	}
	if ec.ContactRef != "igsid-of-"+account.ID {
		t.Errorf("ContactRef = %q, want the contact's IGSID", ec.ContactRef)
	}
	if ec.EntryType != shared.EntryTypeInstagram {
		t.Errorf("EntryType = %q", ec.EntryType)
	}
	if ec.LastInboundAt == nil {
		t.Error("LastInboundAt should be populated; it anchors the 24h window")
	}
}

func TestChannelAdapter_SendUsesTheOwningAccount(t *testing.T) {
	first := connectedAccount()

	second := connectedAccount()
	second.ID = "acct-2"
	second.IGUserID = "17841400000000002"
	second.Username = "other-brand"
	second.AccessToken = "token-acct-2"

	f := newAdapterFixture(t, []*igdomain.Account{first, second}, openWindow())

	for _, account := range []*igdomain.Account{first, second} {
		f.messaging.Sent = nil

		ec, err := f.adapter.ResolveEntry(context.Background(), "conv-"+account.ID)
		if err != nil {
			t.Fatalf("%s: ResolveEntry: %v", account.Username, err)
		}
		if _, err := f.adapter.SendText(context.Background(), ec, conversation.SendTextRequest{Body: "hi"}); err != nil {
			t.Fatalf("%s: SendText: %v", account.Username, err)
		}

		if len(f.messaging.Sent) != 1 {
			t.Fatalf("%s: got %d sends, want 1", account.Username, len(f.messaging.Sent))
		}
		sent := f.messaging.Sent[0]
		if sent.IGUserID != account.IGUserID {
			t.Errorf("%s: sent from IG id %q, want %q", account.Username, sent.IGUserID, account.IGUserID)
		}
		if sent.Token != account.AccessToken {
			t.Errorf("%s: sent with token %q, want %q", account.Username, sent.Token, account.AccessToken)
		}
		if sent.Recipient != "igsid-of-"+account.ID {
			t.Errorf("%s: recipient = %q", account.Username, sent.Recipient)
		}
	}
}

func TestChannelAdapter_WindowState(t *testing.T) {
	account := connectedAccount()

	t.Run("open within 24h", func(t *testing.T) {
		f := newAdapterFixture(t, []*igdomain.Account{account}, openWindow())
		ec, _ := f.adapter.ResolveEntry(context.Background(), "conv-"+account.ID)

		window, err := f.adapter.WindowState(context.Background(), ec)
		if err != nil {
			t.Fatalf("WindowState: %v", err)
		}
		if !window.Open {
			t.Error("window should be open one hour after the last inbound message")
		}
		if window.ExpiresAt == nil {
			t.Fatal("an OPEN window must report its deadline so the UI can count down")
		}
		if got := window.ExpiresAt.Sub(*ec.LastInboundAt); got != igdomain.MessagingWindow {
			t.Errorf("window length = %v, want %v", got, igdomain.MessagingWindow)
		}
	})

	t.Run("closed after 24h", func(t *testing.T) {
		f := newAdapterFixture(t, []*igdomain.Account{account}, closedWindow())
		ec, _ := f.adapter.ResolveEntry(context.Background(), "conv-"+account.ID)

		window, err := f.adapter.WindowState(context.Background(), ec)
		if err != nil {
			t.Fatalf("WindowState: %v", err)
		}
		if window.Open {
			t.Error("window should be closed 25 hours after the last inbound message")
		}
		if window.Reason != conversation.WindowReasonExpired {
			t.Errorf("reason = %q, want expired", window.Reason)
		}
		if window.ExpiresAt != nil {
			t.Error("a lapsed clock must not report a time: on a closed window a time means a countdown to reopening")
		}
	})

	t.Run("never open without an inbound message", func(t *testing.T) {
		f := newAdapterFixture(t, []*igdomain.Account{account}, nil)
		ec, _ := f.adapter.ResolveEntry(context.Background(), "conv-"+account.ID)

		window, err := f.adapter.WindowState(context.Background(), ec)
		if err != nil {
			t.Fatalf("WindowState: %v", err)
		}
		if window.Open {
			t.Error("Instagram forbids initiating a conversation, so the window must be closed")
		}
	})
}

func TestChannelAdapter_SendRejectedWhenWindowClosed(t *testing.T) {
	account := connectedAccount()
	f := newAdapterFixture(t, []*igdomain.Account{account}, closedWindow())

	ec, _ := f.adapter.ResolveEntry(context.Background(), "conv-"+account.ID)
	_, err := f.adapter.SendText(context.Background(), ec, conversation.SendTextRequest{Body: "too late"})

	if !errors.Is(err, conversation.ErrOutboundWindowClosed) {
		t.Fatalf("err = %v, want ErrOutboundWindowClosed", err)
	}
	if len(f.messaging.Sent) != 0 {
		t.Errorf("a closed window must not reach the Send API; got %d calls", len(f.messaging.Sent))
	}
}

func TestChannelAdapter_TextLimitIsBytes(t *testing.T) {
	account := connectedAccount()
	f := newAdapterFixture(t, []*igdomain.Account{account}, openWindow())
	ec, _ := f.adapter.ResolveEntry(context.Background(), "conv-"+account.ID)

	emoji := strings.Repeat("😀", 400)
	if len([]rune(emoji)) >= igdomain.MaxTextBytes {
		t.Fatalf("fixture is not exercising the byte/rune difference: %d runes", len([]rune(emoji)))
	}
	if len(emoji) <= igdomain.MaxTextBytes {
		t.Fatalf("fixture is not over the byte limit: %d bytes", len(emoji))
	}

	if _, err := f.adapter.SendText(context.Background(), ec, conversation.SendTextRequest{Body: emoji}); !errors.Is(err, igdomain.ErrTextTooLong) {
		t.Fatalf("err = %v, want ErrTextTooLong", err)
	}

	f.messaging.Sent = nil
	ok := strings.Repeat("a", igdomain.MaxTextBytes-1)
	if _, err := f.adapter.SendText(context.Background(), ec, conversation.SendTextRequest{Body: ok}); err != nil {
		t.Fatalf("a 999-byte message was rejected: %v", err)
	}
}

func TestChannelAdapter_SendMediaRequiresURL(t *testing.T) {
	account := connectedAccount()
	f := newAdapterFixture(t, []*igdomain.Account{account}, openWindow())
	ec, _ := f.adapter.ResolveEntry(context.Background(), "conv-"+account.ID)

	_, err := f.adapter.SendMedia(context.Background(), ec, conversation.SendMediaRequest{
		Kind:  "image",
		Bytes: []byte("raw"),
	})
	if !errors.Is(err, conversation.ErrCapabilityUnsupported) {
		t.Fatalf("err = %v, want ErrCapabilityUnsupported", err)
	}
}

func TestChannelAdapter_SendMediaRejectsUnsupportedMIME(t *testing.T) {
	account := connectedAccount()
	f := newAdapterFixture(t, []*igdomain.Account{account}, openWindow())
	ec, _ := f.adapter.ResolveEntry(context.Background(), "conv-"+account.ID)

	_, err := f.adapter.SendMedia(context.Background(), ec, conversation.SendMediaRequest{
		Kind:     "image",
		URL:      "https://cdn.example/a.gif",
		MIMEType: "image/gif",
	})
	if !errors.Is(err, conversation.ErrCapabilityUnsupported) {
		t.Fatalf("gif accepted: err = %v", err)
	}

	if _, err := f.adapter.SendMedia(context.Background(), ec, conversation.SendMediaRequest{
		Kind:     "image",
		URL:      "https://cdn.example/a.jpg",
		MIMEType: "image/jpeg",
	}); err != nil {
		t.Fatalf("jpeg rejected: %v", err)
	}
}

func TestChannelAdapter_SendMediaUnknownKind(t *testing.T) {
	account := connectedAccount()
	f := newAdapterFixture(t, []*igdomain.Account{account}, openWindow())
	ec, _ := f.adapter.ResolveEntry(context.Background(), "conv-"+account.ID)

	_, err := f.adapter.SendMedia(context.Background(), ec, conversation.SendMediaRequest{
		Kind: "hologram",
		URL:  "https://cdn.example/x",
	})
	if !errors.Is(err, conversation.ErrCapabilityUnsupported) {
		t.Fatalf("err = %v, want ErrCapabilityUnsupported", err)
	}
}

func TestChannelAdapter_RefusesAccountWithoutMessagingScope(t *testing.T) {
	account := connectedAccount()
	account.GrantedScopes = []string{igdomain.ScopeBasic}

	f := newAdapterFixture(t, []*igdomain.Account{account}, openWindow())
	ec, _ := f.adapter.ResolveEntry(context.Background(), "conv-"+account.ID)

	if _, err := f.adapter.SendText(context.Background(), ec, conversation.SendTextRequest{Body: "hi"}); err == nil {
		t.Fatal("send succeeded without the messaging scope")
	}
	if len(f.messaging.Sent) != 0 {
		t.Error("must not call the Send API without the messaging scope")
	}
}

func TestChannelAdapter_DeadTokenMarksAccountForReconnect(t *testing.T) {
	account := connectedAccount()
	f := newAdapterFixture(t, []*igdomain.Account{account}, openWindow())

	f.messaging.SendTextFn = func(context.Context, string, string, igdomain.SendTextInput) (*igdomain.SendResult, error) {
		return nil, &meta.Error{Code: meta.CodeAccessTokenError, Message: "Error validating access token"}
	}

	ec, _ := f.adapter.ResolveEntry(context.Background(), "conv-"+account.ID)
	if _, err := f.adapter.SendText(context.Background(), ec, conversation.SendTextRequest{Body: "hi"}); err == nil {
		t.Fatal("expected the send to fail")
	}

	if len(f.accounts.StatusUpdates) != 1 || f.accounts.StatusUpdates[0] != igdomain.StatusTokenExpired {
		t.Fatalf("status updates = %v, want one TOKEN_EXPIRED", f.accounts.StatusUpdates)
	}
}

func TestChannelAdapter_ClosedWindowUpstreamSurfacesSentinel(t *testing.T) {
	account := connectedAccount()
	f := newAdapterFixture(t, []*igdomain.Account{account}, openWindow())

	f.messaging.SendTextFn = func(context.Context, string, string, igdomain.SendTextInput) (*igdomain.SendResult, error) {
		return nil, &meta.Error{Code: meta.CodeWindowClosed, Message: "outside allowed window"}
	}

	ec, _ := f.adapter.ResolveEntry(context.Background(), "conv-"+account.ID)
	_, err := f.adapter.SendText(context.Background(), ec, conversation.SendTextRequest{Body: "hi"})

	if !errors.Is(err, conversation.ErrOutboundWindowClosed) {
		t.Fatalf("err = %v, want it to wrap ErrOutboundWindowClosed", err)
	}
}

func TestChannelAdapter_RecordsOutboundClock(t *testing.T) {
	account := connectedAccount()
	f := newAdapterFixture(t, []*igdomain.Account{account}, openWindow())

	ec, _ := f.adapter.ResolveEntry(context.Background(), "conv-"+account.ID)
	if _, err := f.adapter.SendText(context.Background(), ec, conversation.SendTextRequest{Body: "hi"}); err != nil {
		t.Fatalf("SendText: %v", err)
	}
	if f.convs.OutboundRecorded != 1 {
		t.Errorf("outbound clock advanced %d times, want 1", f.convs.OutboundRecorded)
	}
}

func TestChannelAdapter_OptionalCapabilities(t *testing.T) {
	f := newAdapterFixture(t, []*igdomain.Account{connectedAccount()}, openWindow())

	if _, ok := f.adapter.(conversation.ReactingAdapter); !ok {
		t.Error("Instagram supports reactions, so the adapter should implement ReactingAdapter")
	}
	if _, ok := f.adapter.(conversation.PresenceAdapter); !ok {
		t.Error("Instagram supports typing/seen, so the adapter should implement PresenceAdapter")
	}
}

func TestAdapterRegistry_RoutesByEntryType(t *testing.T) {
	f := newAdapterFixture(t, []*igdomain.Account{connectedAccount()}, openWindow())
	registry := conversation.NewAdapterRegistry(f.adapter)

	got, err := registry.For(shared.EntryTypeInstagram)
	if err != nil {
		t.Fatalf("For(instagram): %v", err)
	}
	if got.EntryType() != shared.EntryTypeInstagram {
		t.Errorf("resolved adapter for %q", got.EntryType())
	}
	if !registry.Has(shared.EntryTypeInstagram) {
		t.Error("Has(instagram) = false")
	}

	if _, err := registry.For(shared.EntryTypeWhatsApp); !errors.Is(err, conversation.ErrNoAdapterForEntryType) {
		t.Errorf("For(whatsapp) err = %v, want ErrNoAdapterForEntryType", err)
	}
	if registry.Has(shared.EntryTypeWhatsApp) {
		t.Error("Has(whatsapp) = true, but WhatsApp has not been migrated")
	}
}
