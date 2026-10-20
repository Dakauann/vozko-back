package instagram

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/conversation"
	igdomain "vozko/domain/instagram"
)

func batchOf(t *testing.T, adapter conversation.ChannelAdapter) conversation.WindowStatesAdapter {
	t.Helper()
	batch, ok := adapter.(conversation.WindowStatesAdapter)
	if !ok {
		t.Fatal("the instagram adapter must report window states for a page of entries")
	}
	return batch
}

func accountWith(id string, edit func(*igdomain.Account)) *igdomain.Account {
	account := connectedAccount()
	account.ID = id
	if edit != nil {
		edit(account)
	}
	return account
}

func TestWindowStatesMatchTheSingleEntryRule(t *testing.T) {
	cases := []struct {
		name        string
		account     *igdomain.Account
		lastInbound *time.Time
		wantOpen    bool
		wantReason  conversation.WindowClosedReason
	}{
		{"open", accountWith("open", nil), openWindow(), true, conversation.WindowReasonNone},
		{"expired", accountWith("expired", nil), closedWindow(), false, conversation.WindowReasonExpired},
		{"no inbound", accountWith("silent", nil), nil, false, conversation.WindowReasonNoInbound},
		{"account disconnected", accountWith("down", func(a *igdomain.Account) { a.Status = igdomain.StatusTokenExpired }), openWindow(), false, conversation.WindowReasonChannelUnavailable},
		{"messaging scope revoked", accountWith("scope", func(a *igdomain.Account) { a.GrantedScopes = []string{igdomain.ScopeBasic} }), openWindow(), false, conversation.WindowReasonChannelUnavailable},
		{"token missing", accountWith("token", func(a *igdomain.Account) { a.AccessToken = "" }), openWindow(), false, conversation.WindowReasonChannelUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newAdapterFixture(t, []*igdomain.Account{tc.account}, tc.lastInbound)
			entryID := "conv-" + tc.account.ID
			ctx := context.Background()

			ec, err := f.adapter.ResolveEntry(ctx, entryID)
			if err != nil {
				t.Fatal(err)
			}
			single, err := f.adapter.WindowState(ctx, ec)
			if err != nil {
				t.Fatal(err)
			}
			states, err := batchOf(t, f.adapter).WindowStates(ctx, []string{entryID})
			if err != nil {
				t.Fatal(err)
			}
			got, ok := states[entryID]
			if !ok {
				t.Fatalf("no state for %s", entryID)
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
	accounts := []*igdomain.Account{accountWith("a", nil), accountWith("b", nil), accountWith("c", nil)}
	f := newAdapterFixture(t, accounts, openWindow())
	ids := []string{"conv-a", "conv-b", "conv-c", "conv-a"}

	states, err := batchOf(t, f.adapter).WindowStates(context.Background(), ids)
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 3 {
		t.Fatalf("states = %+v, want one per distinct entry", states)
	}
	if f.convs.FindByIDsCalls != 1 || f.accounts.FindByIDsCalls != 1 {
		t.Fatalf("conversation batches = %d, account batches = %d, want 1 each", f.convs.FindByIDsCalls, f.accounts.FindByIDsCalls)
	}
	if f.convs.FindByIDCalls != 0 || f.accounts.FindByIDCalls != 0 {
		t.Fatalf("per row lookups: conversations %d, accounts %d", f.convs.FindByIDCalls, f.accounts.FindByIDCalls)
	}
}

func TestWindowStatesLeaveUnknownEntriesOut(t *testing.T) {
	f := newAdapterFixture(t, []*igdomain.Account{accountWith("a", nil)}, openWindow())

	states, err := batchOf(t, f.adapter).WindowStates(context.Background(), []string{"conv-a", "conv-gone"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := states["conv-gone"]; ok {
		t.Fatalf("an unknown entry got a state: %+v", states)
	}
	if !states["conv-a"].Open {
		t.Fatalf("known entry = %+v", states["conv-a"])
	}
}

func TestWindowStatesCloseEntriesWhoseAccountIsGone(t *testing.T) {
	f := newAdapterFixture(t, []*igdomain.Account{accountWith("a", nil)}, openWindow())
	f.accounts.FindByIDFn = func(context.Context, string) (*igdomain.Account, error) { return nil, igdomain.ErrAccountNotFound }

	states, err := batchOf(t, f.adapter).WindowStates(context.Background(), []string{"conv-a"})
	if err != nil {
		t.Fatal(err)
	}
	if got := states["conv-a"]; got.Open || got.Reason != conversation.WindowReasonChannelUnavailable {
		t.Fatalf("state = %+v, want closed channel_unavailable", got)
	}
}

func TestWindowStatesReturnLookupErrors(t *testing.T) {
	boom := errors.New("db down")
	for _, broken := range []string{"conversations", "accounts"} {
		t.Run(broken, func(t *testing.T) {
			f := newAdapterFixture(t, []*igdomain.Account{accountWith("a", nil)}, openWindow())
			if broken == "conversations" {
				f.convs.FindByIDsErr = boom
			} else {
				f.accounts.FindByIDsErr = boom
			}
			if _, err := batchOf(t, f.adapter).WindowStates(context.Background(), []string{"conv-a"}); !errors.Is(err, boom) {
				t.Fatalf("err = %v, want the lookup error", err)
			}
		})
	}
}
