package facebook

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/channel"
	"vozko/domain/conversation"
	fbdomain "vozko/domain/facebook"
)

func TestWindowStatesMatchTheSingleEntryRule(t *testing.T) {
	cases := []struct {
		name       string
		approved   bool
		ago        time.Duration
		edit       func(*fbdomain.Page)
		wantOpen   bool
		wantReason conversation.WindowClosedReason
		wantTier   channel.WindowTier
	}{
		{name: "open", ago: time.Hour, wantOpen: true, wantTier: channel.WindowTierStandard},
		{name: "expired", ago: 25 * time.Hour, wantReason: conversation.WindowReasonExpired},
		{name: "human agent tier", approved: true, ago: 25 * time.Hour, wantOpen: true, wantTier: channel.WindowTierHuman},
		{name: "page disconnected", ago: time.Hour, edit: func(p *fbdomain.Page) { p.Status = fbdomain.StatusTokenRevoked }, wantReason: conversation.WindowReasonChannelUnavailable},
		{name: "messaging task missing", ago: time.Hour, edit: func(p *fbdomain.Page) { p.Tasks = nil }, wantReason: conversation.WindowReasonChannelUnavailable},
		{name: "page token missing", ago: time.Hour, edit: func(p *fbdomain.Page) { p.PageToken = "" }, wantReason: conversation.WindowReasonChannelUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newAdapterFixture(t, tc.approved, tc.ago)
			if tc.edit != nil {
				tc.edit(f.pages.byID["page-1"])
			}
			ctx := context.Background()

			single, err := f.adapter.WindowState(ctx, f.ec)
			if err != nil {
				t.Fatal(err)
			}
			states, err := f.adapter.WindowStates(ctx, []string{f.ec.EntryID})
			if err != nil {
				t.Fatal(err)
			}
			got, ok := states[f.ec.EntryID]
			if !ok {
				t.Fatalf("no state for %s", f.ec.EntryID)
			}
			if got.Open != tc.wantOpen || got.Reason != tc.wantReason || got.Tier != tc.wantTier {
				t.Fatalf("batch = %+v, want open=%v reason=%q tier=%q", got, tc.wantOpen, tc.wantReason, tc.wantTier)
			}
			if !got.Equal(single) {
				t.Fatalf("batch %+v disagrees with the single entry %+v", got, single)
			}
		})
	}
}

func TestWindowStatesLoadAPageInOneBatchPerTable(t *testing.T) {
	f := newAdapterFixture(t, false, time.Hour)
	second, _ := f.convs.FindOrCreate(context.Background(), "ws-1", "page-1", "contact-two")
	ids := []string{f.ec.EntryID, second.ID, f.ec.EntryID}
	f.pages.findByIDCalls, f.convs.findByIDCalls = 0, 0

	states, err := f.adapter.WindowStates(context.Background(), ids)
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 2 {
		t.Fatalf("states = %+v, want one per distinct entry", states)
	}
	if f.convs.findByIDsCalls != 1 || f.pages.findByIDsCalls != 1 {
		t.Fatalf("conversation batches = %d, page batches = %d, want 1 each", f.convs.findByIDsCalls, f.pages.findByIDsCalls)
	}
	if f.convs.findByIDCalls != 0 || f.pages.findByIDCalls != 0 {
		t.Fatalf("per row lookups: conversations %d, pages %d", f.convs.findByIDCalls, f.pages.findByIDCalls)
	}
}

func TestWindowStatesCloseEntriesWhosePageIsGone(t *testing.T) {
	f := newAdapterFixture(t, false, time.Hour)
	f.pages.deleted["page-1"] = true

	states, err := f.adapter.WindowStates(context.Background(), []string{f.ec.EntryID, "conv-unknown"})
	if err != nil {
		t.Fatal(err)
	}
	if got := states[f.ec.EntryID]; got.Open || got.Reason != conversation.WindowReasonChannelUnavailable {
		t.Fatalf("state = %+v, want closed channel_unavailable", got)
	}
	if _, ok := states["conv-unknown"]; ok {
		t.Fatal("an unknown entry got a state")
	}
}

func TestWindowStatesReturnLookupErrors(t *testing.T) {
	boom := errors.New("db down")
	for _, broken := range []string{"conversations", "pages"} {
		t.Run(broken, func(t *testing.T) {
			f := newAdapterFixture(t, false, time.Hour)
			if broken == "conversations" {
				f.convs.findByIDsErr = boom
			} else {
				f.pages.findByIDsErr = boom
			}
			if _, err := f.adapter.WindowStates(context.Background(), []string{f.ec.EntryID}); !errors.Is(err, boom) {
				t.Fatalf("err = %v, want the lookup error", err)
			}
		})
	}
}
