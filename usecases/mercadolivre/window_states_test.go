package mercadolivre

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/conversation"
	mldomain "vozko/domain/mercadolivre"
)

func TestWindowStatesMatchTheSingleEntryRule(t *testing.T) {
	cases := []struct {
		name     string
		mediated bool
		mutate   func(f *adapterFixture)
		open     bool
		reason   conversation.WindowClosedReason
		detail   string
	}{
		{name: "open thread", open: true},
		{name: "mediated thread with budget left", mediated: true, open: true},
		{name: "budget exhausted", mediated: true, mutate: func(f *adapterFixture) {
			_, _ = f.conversations.ClaimOutbound(context.Background(), f.conv.ID)
		}, reason: conversation.WindowReasonBudgetExhausted},
		{name: "provider blocked with detail", mutate: func(f *adapterFixture) {
			_ = f.conversations.ApplyStatus(context.Background(), f.conv.ID, mldomain.ThreadState{
				Status: "blocked", Substatus: "blocked_by_mediation", AgentMediated: boolPtr(false), ReadAt: testNow,
			})
		}, reason: conversation.WindowReasonProviderBlocked, detail: "blocked_by_mediation"},
		{name: "mediation unknown", mutate: func(f *adapterFixture) {
			_ = f.conversations.ApplyStatus(context.Background(), f.conv.ID, mldomain.ThreadState{Status: "active", ReadAt: testNow})
		}, reason: conversation.WindowReasonChannelUnavailable},
		{name: "account disconnected", mutate: func(f *adapterFixture) {
			_ = f.accounts.UpdateStatus(context.Background(), "acct-1", mldomain.StatusNeedsReconnect, "x")
		}, reason: conversation.WindowReasonChannelUnavailable},
		{name: "send permission lost", mutate: func(f *adapterFixture) {
			_ = f.accounts.UpdateCapabilities(context.Background(), "acct-1", mldomain.AccountCapabilities{CanReadMessages: true})
		}, reason: conversation.WindowReasonChannelUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newAdapterFixture(t, tc.mediated, true)
			if tc.mutate != nil {
				tc.mutate(f)
			}
			ctx := context.Background()
			single, err := f.adapter.WindowState(ctx, f.entry(t))
			if err != nil {
				t.Fatal(err)
			}
			states, err := f.adapter.WindowStates(ctx, []string{f.conv.ID})
			if err != nil {
				t.Fatal(err)
			}
			got, ok := states[f.conv.ID]
			if !ok {
				t.Fatalf("no state for %s", f.conv.ID)
			}
			if got.Open != tc.open || got.Reason != tc.reason || got.ProviderDetail != tc.detail {
				t.Fatalf("batch = %+v, want open=%v reason=%q detail=%q", got, tc.open, tc.reason, tc.detail)
			}
			if !got.Equal(single) {
				t.Fatalf("batch %+v disagrees with the single entry %+v", got, single)
			}
		})
	}
}

func TestWindowStatesLoadAPageInOneBatchPerTable(t *testing.T) {
	f := newAdapterFixture(t, false, true)
	f.accounts.findByIDCalls, f.conversations.findByIDCalls = 0, 0

	states, err := f.adapter.WindowStates(context.Background(), []string{f.conv.ID, f.conv.ID, "conv-gone"})
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 || !states[f.conv.ID].Open {
		t.Fatalf("states = %+v", states)
	}
	if f.conversations.findByIDsCalls != 1 || f.accounts.findByIDsCalls != 1 {
		t.Fatalf("batches: conversations %d, accounts %d, want 1 each", f.conversations.findByIDsCalls, f.accounts.findByIDsCalls)
	}
	if f.conversations.findByIDCalls != 0 || f.accounts.findByIDCalls != 0 {
		t.Fatalf("per row lookups: conversations %d, accounts %d", f.conversations.findByIDCalls, f.accounts.findByIDCalls)
	}
}

func TestWindowStatesNeverQueueProviderRereads(t *testing.T) {
	f := newAdapterFixture(t, false, true)
	before := f.refreshQueue.total()

	if _, err := f.adapter.WindowStates(context.Background(), []string{f.conv.ID}); err != nil {
		t.Fatal(err)
	}
	if after := f.refreshQueue.total(); after != before {
		t.Fatalf("a page read queued %d thread rereads", after-before)
	}
}

func TestWindowStatesCloseEntriesWhoseAccountIsGone(t *testing.T) {
	f := newAdapterFixture(t, false, true)
	f.accounts.deleted["acct-1"] = true

	states, err := f.adapter.WindowStates(context.Background(), []string{f.conv.ID})
	if err != nil {
		t.Fatal(err)
	}
	if got := states[f.conv.ID]; got.Open || got.Reason != conversation.WindowReasonChannelUnavailable {
		t.Fatalf("state = %+v, want closed channel_unavailable", got)
	}
}

func TestWindowStatesReturnLookupErrors(t *testing.T) {
	boom := errors.New("db down")
	for _, broken := range []string{"conversations", "accounts"} {
		t.Run(broken, func(t *testing.T) {
			f := newAdapterFixture(t, false, true)
			if broken == "conversations" {
				f.conversations.findByIDsErr = boom
			} else {
				f.accounts.failFind = boom
			}
			if _, err := f.adapter.WindowStates(context.Background(), []string{f.conv.ID}); !errors.Is(err, boom) {
				t.Fatalf("err = %v, want the lookup error", err)
			}
		})
	}
}
