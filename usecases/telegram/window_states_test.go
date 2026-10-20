package telegram

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/conversation"
	tgdomain "vozko/domain/telegram"
)

func batchOf(t *testing.T, adapter conversation.ChannelAdapter) conversation.WindowStatesAdapter {
	t.Helper()
	batch, ok := adapter.(conversation.WindowStatesAdapter)
	if !ok {
		t.Fatal("the telegram adapter must report window states for a page of entries")
	}
	return batch
}

func hoursAgo(h int) *time.Time {
	at := time.Now().UTC().Add(-time.Duration(h) * time.Hour)
	return &at
}

func TestWindowStatesMatchTheSingleEntryRule(t *testing.T) {
	revoked := botAccount()
	revoked.Status = tgdomain.StatusRevoked
	tokenless := botAccount()
	tokenless.BotToken = ""
	businessRevoked := businessAccount(false)

	cases := []struct {
		name        string
		account     *tgdomain.Account
		blocked     bool
		lastInbound *time.Time
		wantOpen    bool
		wantReason  conversation.WindowClosedReason
	}{
		{"bot open without a clock", botAccount(), false, hoursAgo(24 * 7), true, conversation.WindowReasonNone},
		{"bot contact blocked", botAccount(), true, hoursAgo(1), false, conversation.WindowReasonContactBlocked},
		{"bot account revoked", revoked, false, hoursAgo(1), false, conversation.WindowReasonChannelUnavailable},
		{"bot token missing", tokenless, false, hoursAgo(1), false, conversation.WindowReasonChannelUnavailable},
		{"business open", businessAccount(true), false, hoursAgo(1), true, conversation.WindowReasonNone},
		{"business expired", businessAccount(true), false, hoursAgo(25), false, conversation.WindowReasonExpired},
		{"business no inbound", businessAccount(true), false, nil, false, conversation.WindowReasonNoInbound},
		{"business reply revoked", businessRevoked, false, hoursAgo(1), false, conversation.WindowReasonReplyRevoked},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			conv := conversationFor(5041234567)
			conv.LastCustomerMessageAt = tc.lastInbound
			contact := &tgdomain.Contact{ID: "contact-1", TGUserID: 5041234567, Blocked: tc.blocked}
			adapter, _, _, _, _, _ := newAdapter(tc.account, conv, contact)
			ctx := context.Background()

			ec, err := adapter.ResolveEntry(ctx, conv.ID)
			if err != nil {
				t.Fatal(err)
			}
			single, err := adapter.WindowState(ctx, ec)
			if err != nil {
				t.Fatal(err)
			}
			states, err := batchOf(t, adapter).WindowStates(ctx, []string{conv.ID})
			if err != nil {
				t.Fatal(err)
			}
			got, ok := states[conv.ID]
			if !ok {
				t.Fatalf("no state for %s", conv.ID)
			}
			if got.Open != tc.wantOpen || got.Reason != tc.wantReason {
				t.Fatalf("batch = %+v, want open=%v reason=%q", got, tc.wantOpen, tc.wantReason)
			}
			if !got.Equal(single) {
				t.Fatalf("batch %+v disagrees with the single entry %+v", got, single)
			}
		})
	}
}

func TestWindowStatesLoadAPageInOneBatchPerTable(t *testing.T) {
	conv := conversationFor(5041234567)
	contact := &tgdomain.Contact{ID: "contact-1", TGUserID: 5041234567}
	adapter, accounts, contacts, conversations, _, _ := newAdapter(botAccount(), conv, contact)

	states, err := batchOf(t, adapter).WindowStates(context.Background(), []string{"conv-1", "conv-1", ""})
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 || !states["conv-1"].Open {
		t.Fatalf("states = %+v", states)
	}
	if conversations.FindByIDsCalls != 1 || accounts.FindByIDsCalls != 1 || contacts.FindByIDsCalls != 1 {
		t.Fatalf("batches: conversations %d, accounts %d, contacts %d, want 1 each",
			conversations.FindByIDsCalls, accounts.FindByIDsCalls, contacts.FindByIDsCalls)
	}
	if conversations.FindByIDCalls != 0 || accounts.FindByIDCalls != 0 || contacts.FindByIDCalls != 0 {
		t.Fatalf("per row lookups: conversations %d, accounts %d, contacts %d",
			conversations.FindByIDCalls, accounts.FindByIDCalls, contacts.FindByIDCalls)
	}
}

func TestWindowStatesSkipContactsForBusinessAccounts(t *testing.T) {
	conv := conversationFor(5041234567)
	conv.LastCustomerMessageAt = hoursAgo(1)
	adapter, _, contacts, _, _, _ := newAdapter(businessAccount(true), conv, &tgdomain.Contact{ID: "contact-1"})

	if _, err := batchOf(t, adapter).WindowStates(context.Background(), []string{"conv-1"}); err != nil {
		t.Fatal(err)
	}
	if contacts.FindByIDsCalls != 0 {
		t.Fatalf("business mode read %d contact batches, the rule never reads the contact", contacts.FindByIDsCalls)
	}
}

func TestWindowStatesCloseEntriesWithAMissingAccountOrContact(t *testing.T) {
	for _, missing := range []string{"account", "contact"} {
		t.Run(missing, func(t *testing.T) {
			conv := conversationFor(5041234567)
			adapter, accounts, contacts, _, _, _ := newAdapter(botAccount(), conv, &tgdomain.Contact{ID: "contact-1"})
			if missing == "account" {
				accounts.FindByIDFn = func(context.Context, string) (*tgdomain.Account, error) { return nil, tgdomain.ErrAccountNotFound }
			} else {
				contacts.FindByIDFn = func(context.Context, string) (*tgdomain.Contact, error) { return nil, tgdomain.ErrContactNotFound }
			}
			states, err := batchOf(t, adapter).WindowStates(context.Background(), []string{"conv-1"})
			if err != nil {
				t.Fatal(err)
			}
			if got := states["conv-1"]; got.Open || got.Reason != conversation.WindowReasonChannelUnavailable {
				t.Fatalf("state = %+v, want closed channel_unavailable", got)
			}
		})
	}
}

func TestWindowStatesReturnLookupErrors(t *testing.T) {
	boom := errors.New("db down")
	for _, broken := range []string{"conversations", "accounts", "contacts"} {
		t.Run(broken, func(t *testing.T) {
			conv := conversationFor(5041234567)
			adapter, accounts, contacts, conversations, _, _ := newAdapter(botAccount(), conv, &tgdomain.Contact{ID: "contact-1"})
			switch broken {
			case "conversations":
				conversations.FindByIDsErr = boom
			case "accounts":
				accounts.FindByIDsErr = boom
			default:
				contacts.FindByIDsErr = boom
			}
			if _, err := batchOf(t, adapter).WindowStates(context.Background(), []string{"conv-1"}); !errors.Is(err, boom) {
				t.Fatalf("err = %v, want the lookup error", err)
			}
		})
	}
}
